# Compute architecture

Workload adapters describe the capabilities and configuration of `shell`, `browser-chromium`, `desktop-novnc` and `desktop-neko`. They do not choose an isolation mechanism. Runtime backends own provisioning, lifecycle, resources and network policy. Docker over a local Unix socket is implemented; Apple Virtualization.framework and Kubernetes are unsupported in the current Go layer. Reference POC implementations do not establish those backend contracts.

The host CLI invokes the runtime and communicates with an authenticated Go guest agent over a fixed private transport. The agent provides bounded command execution and workspace file operations; browser and desktop capabilities are separate. Shell and headless browser workloads do not require a desktop. Policies for choosing a workload/runtime, profiles, harnesses and durable agent loops are deferred.

The Docker backend creates explicitly owned, labelled containers with non-root UID, read-only root, dropped capabilities, no-new-privileges, bounded resources and private temporary filesystems. It does not expose host mounts or the Docker socket to guests. Network defaults to none. Explicit bridge egress is not an egress allowlist or a microVM boundary. Viewer publication is restricted to loopback by this runtime. These properties depend on Docker and the host enforcing them; see [security boundaries](../compute/security.md).

The clean multi-stage image recipe and its runtime assets live in `infrastructure/images/`; frontend source remains in `web/`. Third-party noVNC, Neko, browser, Xfce, GStreamer and system components retain their native implementations and licensing. [Compute status](../compute/status.md) distinguishes implemented paths from tested behavior and known gaps.
