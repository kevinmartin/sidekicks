import Foundation
import Virtualization
import Darwin

// One process, one first-level Linux VM. NAT is an explicit separate profile; no host shares.
func die(_ message: String) -> Never { fputs(message + "\n", stderr); exit(1) }
let args = CommandLine.arguments
guard args.count == 6 || (args.count == 7 && ["neko", "neko-nat"].contains(args[6])) else { die("usage: runner KERNEL DISK STATE VIEWER_PORT API_PORT [neko|neko-nat]") }
let profile = args.count == 7 ? args[6] : "novnc"
let neko = profile.hasPrefix("neko")
let nat = profile == "neko-nat"
guard VZVirtualMachine.isSupported else { die("Virtualization.framework unavailable in this execution context") }
let state = args[3]
let config = VZVirtualMachineConfiguration()
let boot = VZLinuxBootLoader(kernelURL: URL(fileURLWithPath: args[1]))
boot.commandLine = "console=hvc0 root=/dev/vda rw rootfstype=ext4 init=/microvm-init.py panic=0 loglevel=6 sidekicks.viewer_port=\(args[4]) sidekicks.api_port=\(args[5]) sidekicks.profile=\(profile)"
config.bootLoader = boot
config.cpuCount = 2
config.memorySize = 2 * 1024 * 1024 * 1024
config.platform = VZGenericPlatformConfiguration()
config.entropyDevices = [VZVirtioEntropyDeviceConfiguration()]
let block = VZVirtioBlockDeviceConfiguration(attachment: try VZDiskImageStorageDeviceAttachment(url: URL(fileURLWithPath: args[2]), readOnly: false))
config.storageDevices = [block]
config.socketDevices = [VZVirtioSocketDeviceConfiguration()]
if nat {
    let nic = VZVirtioNetworkDeviceConfiguration()
    nic.macAddress = VZMACAddress.randomLocallyAdministered()
    nic.attachment = VZNATNetworkDeviceAttachment()
    config.networkDevices = [nic]
}
let serial = VZVirtioConsoleDeviceSerialPortConfiguration()
FileManager.default.createFile(atPath: state + "/console.log", contents: nil)
let console = try FileHandle(forWritingTo: URL(fileURLWithPath: state + "/console.log"))
try console.seekToEnd()
serial.attachment = VZFileHandleSerialPortAttachment(fileHandleForReading: nil, fileHandleForWriting: console)
config.serialPorts = [serial]
try config.validate()
let vm = VZVirtualMachine(configuration: config)
signal(SIGPIPE, SIG_IGN)

func writeAll(_ fd: Int32, _ bytes: [UInt8]) -> Bool {
    var offset = 0
    while offset < bytes.count {
        let n = bytes.withUnsafeBytes { Darwin.write(fd, $0.baseAddress!.advanced(by: offset), bytes.count - offset) }
        if n <= 0 { return false }; offset += n
    }
    return true
}
func bridge(_ host: Int32, _ connection: VZVirtioSocketConnection, _ prefix: String) {
    let guest = connection.fileDescriptor
    guard writeAll(guest, Array(prefix.utf8)) else { close(host); return }
    let group = DispatchGroup()
    for (src, dst) in [(host, guest), (guest, host)] {
        group.enter()
        DispatchQueue.global().async {
            var buffer = [UInt8](repeating: 0, count: 65536)
            while true {
                let count = Darwin.read(src, &buffer, buffer.count)
                if count <= 0 || !writeAll(dst, Array(buffer.prefix(count))) { break }
            }
            shutdown(dst, SHUT_WR)
            group.leave()
        }
    }
    group.notify(queue: .main) { close(host); _ = connection }
}
func serve(_ listener: Int32, _ prefix: String) {
    DispatchQueue.global().async {
        while true {
            let client = accept(listener, nil, nil)
            if client < 0 { return }
            DispatchQueue.main.async {
                guard let device = vm.socketDevices.first as? VZVirtioSocketDevice else { close(client); return }
                device.connect(toPort: 8000) { result in
                    switch result {
                    case .success(let connection): bridge(client, connection, prefix)
                    case .failure: close(client)
                    }
                }
            }
        }
    }
}
func tcpListener(_ port: UInt16) -> Int32 {
    let fd = socket(AF_INET, SOCK_STREAM, 0)
    var reuse: Int32 = 1
    guard setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &reuse, socklen_t(MemoryLayout<Int32>.size)) == 0 else { die("SO_REUSEADDR failed") }
    var addr = sockaddr_in()
    addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size); addr.sin_family = sa_family_t(AF_INET)
    addr.sin_port = port.bigEndian; addr.sin_addr.s_addr = inet_addr("127.0.0.1")
    let result = withUnsafePointer(to: &addr) { $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.bind(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) } }
    guard result == 0, listen(fd, 32) == 0 else { die("loopback bind failed on \(port): \(errno)") }
    return fd
}
func unixListener(_ path: String) -> Int32 {
    let fd = socket(AF_UNIX, SOCK_STREAM, 0)
    var addr = sockaddr_un(); addr.sun_family = sa_family_t(AF_UNIX)
    let bytes = Array(path.utf8) + [0]
    guard bytes.count < MemoryLayout.size(ofValue: addr.sun_path) else { die("state path too long for Unix socket") }
    withUnsafeMutableBytes(of: &addr.sun_path) { $0.copyBytes(from: bytes) }
    let result = withUnsafePointer(to: &addr) { $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { Darwin.bind(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) } }
    guard result == 0, listen(fd, 32) == 0 else { die("control socket bind failed: \(errno)") }
    chmod(path, 0o600)
    return fd
}
// Bind before boot. Fail closed if a requested endpoint is occupied.
guard let viewer = UInt16(args[4]), let api = UInt16(args[5]), viewer != api else { die("invalid distinct ports") }
let viewerFD = tcpListener(viewer), apiFD = tcpListener(api)
let rpcFD = unixListener(state + "/rpc.sock")
// Separate private local stream capability. Only the Neko profile exposes framed UDP.
let udpFD: Int32? = (neko && !nat) ? unixListener(state + "/udp.sock") : nil
let stopSource = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
signal(SIGTERM, SIG_IGN)
stopSource.setEventHandler { vm.stop { _ in exit(0) } }
stopSource.resume()
vm.start { result in
    switch result {
    case .success:
        serve(viewerFD, neko ? "tcp:7902\n" : "tcp:6080\n"); serve(apiFD, neko ? "tcp:7792\n" : "tcp:8080\n"); serve(rpcFD, "rpc\n")
        if let udp = udpFD { serve(udp, "udp-v1\n") }
        print("VM started: 2 vCPU / 2 GiB; desktop readiness must be polled separately")
        fflush(stdout)
    case .failure(let error): die("VM start failed: \(error)")
    }
}
RunLoop.main.run()
