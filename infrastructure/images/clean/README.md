# Clean compute image recipes

This single multi-stage Dockerfile has four final targets: `shell`, `browser-chromium`, `desktop-novnc`, `desktop-neko`. It does not inherit local POC images, copy their old orchestration, or depend on ignored historical archives. The two desktops share one Debian Trixie/Xfce/GTK3 runtime layer; the headless Chromium target has no X server, window manager, panel, Files or Terminal desktop. The shell target is lean Debian plus the Go agent.

Each final target copies the prebuilt Go agent **after** its package and upstream-source layers. Editing Go source/binary therefore does not invalidate desktop APT or Neko/noVNC source-build layers. The common base retains the same entrypoint, UID1000, workdir and empty CMD; its intermediate dependency stages intentionally do not yet contain that executable. The source remains `.run/compute-bin/sidekicks-agent` from the repository-root Docker context, allowed by the root `.dockerignore`. The Go agent is the only workload entrypoint. It generates its own private session configuration and owns lifecycle/control. x11vnc, noVNC, Chromium, Firefox, Xfce, Neko and GStreamer remain third-party components in their native languages. No Sidekicks Node or Python adapter runtime is included. A transitive system package may use Python; that is distinct from Sidekicks orchestration.

## Build inputs and usage

Run from the repository root after Make has built `.run/compute-bin/sidekicks-agent` as a static Linux binary **for the selected Docker platform** and `.run/compute-web/neko.js` from the pinned web npm lockfile. The root Docker context must allow those files, `infrastructure/images/assets/desktop/`, and this directory. For example, after the root build prerequisite:

```sh
make build IMAGE_ARCH=arm64
docker build --platform linux/arm64 --target shell -f infrastructure/images/clean/Dockerfile -t sidekicks-compute/shell:clean .
docker build --platform linux/arm64 --target browser-chromium -f infrastructure/images/clean/Dockerfile -t sidekicks-compute/browser-chromium:clean .
docker build --platform linux/arm64 --target desktop-novnc -f infrastructure/images/clean/Dockerfile -t sidekicks-compute/desktop-novnc:clean .
docker build --platform linux/arm64 --target desktop-neko -f infrastructure/images/clean/Dockerfile -t sidekicks-compute/desktop-neko:clean .
```

For amd64, select `IMAGE_ARCH=amd64` and use `--platform linux/amd64` consistently for each build. The agent architecture must match the selected image platform.

The coordinator completed clean builds on Linux ARM64; runtime acceptance remains separately recorded in the current compute evidence. Recipe presence alone is not a portability or all-runtime acceptance claim. The coordinator owns build sequencing and verification; no existing demo is replaced by these commands. All original recipes remain unchanged.

## Pinning and licensing

`bases.json` records official Docker Library multiarch index digests and amd64/arm64 manifests read on2026-10-30. The Dockerfile pins the Debian base and the separate supported Go1.27.3/Trixie Neko builder (the initial1.25.14 descriptor is retained only as historical metadata). Builds use the native target platform; Neko's CGO code is not cross-compiled using a host C compiler. The Go agent remains the root module's separately built binary.

noVNC1.6.0 is fetched from its official GitHub codeload source archive and its SHA256 is mandatory. This is the browser ES-module tree containing `core/rfb.js`, not the npm package's compiled CommonJS `lib/` output. The exact checksum is retained from the reference POC and rechecked on each clean build. The source tree, including its MPL2.0 license, is retained under `/opt/novnc`. `novnc-npm-attempt.json` preserves the first attempt: npm integrity verification passed, but the coordinator's actual build correctly failed the incompatible layout assertion. No APT, base-image or runtime-package change was made for this correction.

Neko3.2.0 is fetched from official GitHub codeload and must match the archived POC source SHA256. It is built as an ordinary upstream Go/CGO server, with development dependencies left in the builder. Apache2.0 license and module manifests are retained under `/usr/share/doc/neko`. The SDK bundle remains pinned by the web lockfile; its license/provenance must accompany distribution of that bundle. Native distro libraries retain Debian copyright/license files. GStreamer codec distribution may require additional review for the destination jurisdiction; this recipe is not a legal determination.

Pinned bases/source releases are reproducible inputs, but Debian APT repositories are not historical snapshots and package versions can change. A build should record its complete package manifest and resulting image digest, then run current checks. This does not promise byte-identical images forever or claim that retained POC images are no longer necessary for historical reproduction.

## Runtime and verification contract

The runtime supplies UID1000, read-only root, bounded private tmpfs, PID/CPU/memory caps, dropped capabilities, no-new-privileges and intentional loopback publications. These restrictions are not replaced by Dockerfile metadata. No host mounts, devices, Docker socket or privileged service is requested. The image declares no implicit VOLUME. No broad browser `--no-sandbox` default is added here; browser sandbox compatibility must pass actual runtime tests or be reported as an explicit limitation.

The Go agent invokes x11vnc's supported `-nowireframe` flag; the Debian binary is exposed at the fixed `/usr/local/bin/x11vnc` path through a symlink. Raw RFB remains loopback. Both desktops use the existing Sidekicks-owned asset paths and Go-compiled session templates. Firefox startup/Home and privileged local new-tab override use `http://127.0.0.1:8081/home`; content-process sandbox settings are not disabled. The AutoConfig internal-module dependency requires actual ESR/new-tab acceptance.

Neko credentials and server/pulse settings are generated per instance by the agent, not baked into the image. Native X11/GStreamer/GTK/Pulse dependencies are retained. There is no Docker-inside-guest or VZ/Kubernetes support claim. Neko media needs the runtime's explicit network/port policy and actual selected-ICE/decode verification. Sound decoding/listening, browser sandbox, all window interactions, actual control release, restart, clean-build size and package inventory remain actual-test gates rather than source-only assertions.
