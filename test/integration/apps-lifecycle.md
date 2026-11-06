# Isolated Browser / Terminal / Files lifecycle

This is an explicitly opted-in, destructive-to-its-own-apps integration test. It creates a **new** task-owned container from a test-only derivative of the exact selected image ID. It has no option to attach to an existing POC or accept a caller-selected container name. Do not run it before the coordinator hands over the candidate image and runtime window.

The inner `TestRealGuestApps` now accepts `SIDEKICKS_APPS_TEST_BROWSER=firefox`. Empty/`chromium` retains the original Chromium default. All existing Chromium assertions remain: fresh unoccupied managed-client display; normal Browser/Terminal/Files launch and mapped-window/PID observation; managed Home title; two minimize/restore rounds preserving the exact client, PID and geometry with one matching window; WM_DELETE_WINDOW; at least 500 ms continuously closed; explicit reopen; one matching client; final three-app state. Firefox runs exactly that matrix, not a relaxed substitute. Chromium's sandbox remains enabled in this test configuration.

The host `TestIsolatedAppsLifecycle` requires:

- `SIDEKICKS_APPS_LIFECYCLE=1` and explicit `SIDEKICKS_APPS_TEST_BROWSER=firefox` or `chromium`.
- `SIDEKICKS_APPS_IMAGE_ID=sha256:<64 lowercase hex>`; caller-supplied tags are rejected. The inspected image must match the expected Go-agent adapter, Linux architecture, entrypoint and empty command, with no implicit volumes. For the test-only derivative build, an existing local `sidekicks-compute/desktop-*` tag must resolve to that exact ID both before and after the build; the resulting base layers and image configuration are also checked.
- The `make integration-apps` entry point always rebuilds the test binary from the current checkout for Linux, matching arm64/amd64, `CGO_ENABLED=0` and `-cover`. Its guard verifies ELF shape and package identity, not freshness: direct `go test` invocation can supply a stale binary and must not be used as candidate acceptance. Scripts, links, non-executable/private files and other package identities are rejected.
- An explicit local absolute `SIDEKICKS_DOCKER_SOCKET`.

After an explicit handoff, run the Make entry point:

```sh
SIDEKICKS_APPS_TEST_BROWSER=firefox \
SIDEKICKS_APPS_IMAGE_ID=sha256:EXACT_APPROVED_IMAGE_ID \
make integration-apps
```

The runtime creates a fresh 2-CPU / 2-GiB / 512-PID container with a read-only root, network-none, no ports, no host binds, non-root UID1000 and initial browser startup disabled. The guest workspace is mounted `noexec` and Docker's copy API rejects its read-only root. The harness therefore builds a **test-only, untagged derivative** from the exact selected local image with one added layer containing only the test binary. It checks that the base layers and image configuration match, and verifies the guest binary SHA-256 before execution. Evidence records both immutable image IDs; this is not execution of the unmodified image. The test derivative is retained for inspection. The coordinator inspects ownership labels, image and immutable container ID before test-only `docker exec`. Docker CLI calls use the explicit Unix socket and a new empty Docker config directory, not personal registry/context configuration. The test binary executes only `TestRealGuestApps` as UID1000, at fixed guest display `:99`, fixed private home and session bus. The inner suite refuses any pre-existing managed app clients.

The test has a 150-second guest timeout and three-minute host-operation budget. Cleanup uses a separate bounded Go-runtime stop/destroy context against only the generated owned name; it does not rely on killing a Docker CLI process to kill a guest command. Cleanup failures fail the host test. No force destroy, Docker prune, image deletion, host mount, host display, firewall or network-setting mutation is used.

Per-run stdout (including failed guest test output), exact image/binary/container metadata, Git revision, dirty status and a digest of the relevant current Go source before and after the test, and a bounded guest coverage profile are saved under `artifacts/compute/apps-lifecycle/<generated-name>/`. The digest detects source changes during that run; it is not an independent attestation tying an arbitrary prebuilt binary to source. The Make prerequisite provides the fresh local build. The app-test summary describes its app matrix; cleanup success is determined by the final host test result. The image is retained. Older POCs, browser sessions and VM disks are not part of this harness.

This is actual XGB/EWMH interaction, not interaction through a human noVNC/WebRTC viewer. It does not establish physical keyboard behavior, mobile UX, browser Home/New Tab policies beyond the loaded Home title, stream encoding/media, audio, network behavior, latency or production isolation certification. Those remain separate tests. Ordinary race/guard tests skip the real guest run and must not be reported as a Firefox acceptance pass.
