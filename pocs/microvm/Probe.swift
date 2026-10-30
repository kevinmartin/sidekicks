import Foundation
import Virtualization

let result: [String: Any] = [
    "framework": "Virtualization.framework",
    "supported": VZVirtualMachine.isSupported,
    "minimumCPUCount": VZVirtualMachineConfiguration.minimumAllowedCPUCount,
    "maximumCPUCount": VZVirtualMachineConfiguration.maximumAllowedCPUCount,
    "minimumMemorySize": VZVirtualMachineConfiguration.minimumAllowedMemorySize,
    "maximumMemorySize": VZVirtualMachineConfiguration.maximumAllowedMemorySize,
    "os": ProcessInfo.processInfo.operatingSystemVersionString
]
let data = try JSONSerialization.data(withJSONObject: result, options: [.prettyPrinted, .sortedKeys])
print(String(decoding: data, as: UTF8.self))
