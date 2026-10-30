# Sidekicks compute

Sidekicks provides a Go-first local compute layer for isolated workloads. It is under development and is **not production-ready**. The current runtime is Docker; Apple Virtualization.framework and Kubernetes backends are not implemented in this layer.

The four workload adapters are `shell`, `browser-chromium`, `desktop-novnc` and `desktop-neko`. Adapter choice describes workload capabilities; the runtime supplies lifecycle and isolation. Profiles, an agent harness and durable services are future work.

## Development

The module's `go 1.25.0` directive is a language and API compatibility baseline, not a recommendation to deploy an unsupported toolchain. This pass uses Go 1.27.3; choose a patched toolchain within the [official Go support window](https://go.dev/doc/devel/release#policy). A build with the minimum-version toolchain has not been verified here.

Go, Node and a local Docker Engine are required for the full verification path. From the repository root:

```sh
make setup       # install pinned development tools and web dependencies
make check       # formatting, static analysis, race tests, coverage and Linux builds
make build       # host CLI and Linux guest agent
make images      # build the four compute images
make verify      # check and verify uniquely tagged candidates, without promotion
make release-verify  # explicit advisory and local-image release checks
```

`make help` lists the scoped integration and candidate checks. `make verify` binds shell/browser integration, desktop application lifecycle, viewer E2E and synthetic Neko audio to the exact candidate image IDs; release advisory checks remain a separate gate. Image builds and release checks need their documented dependencies and permissions; they do not silently turn missing prerequisites into passes. Tests create owned disposable resources. Do not run real integration or E2E against personal containers.

The CLI starts with `sidekicks adapters`; lifecycle and guest operations are described in [compute status](docs/compute/status.md). Network defaults to none. Explicit Docker bridge egress can reach host and LAN services; local viewer endpoints are not a general Internet-facing authentication system. Read the [security boundaries](docs/compute/security.md) before using the layer.

## Repository

- `cmd/` contains the host CLI and guest agent.
- `internal/compute/` contains workload contracts, guest services, desktop control and the Docker backend.
- `web/` contains browser-side viewer and fixture sources.
- `infrastructure/images/` contains the clean image recipe, its assets and pinned upstream module metadata.
- `scripts/compute/` and `test/` contain development, security and verification tooling.

The earlier POC source is preserved in the local `reference/poc` branch and `poc-baseline-2026-10-30` tag. Ignored VM disks, runtime state and evidence are not contained in that branch and remain local; they are not an off-host backup. See [POC preservation](docs/reference-poc/preservation.md). Nothing has been pushed by this work.

See [architecture](docs/architecture/overview.md), [desktop control](docs/architecture/desktop-stack.md), and [current status](docs/compute/status.md) for supported behavior, evidence and open limitations.
