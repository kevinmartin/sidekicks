# Local release evidence and dependency verification

This tool runs only read-only local image inspection/scanning and owned local checks. It never builds images, starts containers, publishes/uploads an image or report, prunes Docker, or alters the POCs. By default, run it only after the coordinator freezes the four clean compute tags. The verifier resolves those tags once, scans their immutable image IDs, and checks that the tag IDs remain unchanged afterward.

```sh
make release-tools      # Explicit official checksum-pinned Trivy0.76.1 download
make release-manifest   # Offline advisory-free metadata; needs local Docker read access
make release-verify     # Explicit online advisory/DB refresh and local image scans
```

To verify separately built candidates without changing the clean tags, pass an explicit JSON map to the verifier. Replace every placeholder with a complete distinct lowercase 64-hex image ID; do not paste the placeholders literally:

```sh
go run ./scripts/compute/security -mode verify -online -socket "$SIDEKICKS_DOCKER_SOCKET" \
  -images '{"shell":"sha256:<shell-id>","browser-chromium":"sha256:<browser-id>","desktop-novnc":"sha256:<novnc-id>","desktop-neko":"sha256:<neko-id>"}'
```

`-images` is also accepted with `-mode manifest`. It must contain exactly the four adapter keys with distinct immutable IDs: missing, duplicate or extra keys, mutable tags, uppercase IDs, duplicate IDs and trailing JSON are rejected before image inspection. Explicit selection inspects those local IDs before and after the operation, verifies each image declares the matching adapter, and requires each scanner result to report the requested image ID in both its artifact name and metadata. Any mismatch or missing identity is incomplete, never a pass. Reports identify the selection mode and each scanned ID; image environment values are checked in memory and not saved. `-images` is not accepted by the offline classifier. Omit it to retain the default clean-tag workflow.

Each run creates a new `artifacts/compute/release/<UTC timestamp>/` directory. Existing evidence is never overwritten. The report records current Git commit plus dirty/untracked **booleans**, never raw Git status/private filenames; bounded hashes of owned Go/web/build/image/shared-desktop source inputs (including the reviewed Neko `upstream-modules` overlay, its module/sum files and review manifest); hashes/build info of local agent/web/tool inputs; and exact image IDs, layer digests, architecture, size and selected non-secret image configuration. Full environment/command/label configuration is not written from scanner metadata. Source hashes describe the working tree and local build inputs. This is **self-recorded metadata**, not a signed attestation, proof of an image's source/binary correspondence, or remote backup.

## Verification coverage

- Pinned govulncheck v1.9.0 queries current Go advisory data for reachable owned Go source/standard-library/module findings. It does not cover Debian/native libraries, Chromium/Firefox, or the separate upstream Neko executable.
- npm audit queries the official public registry against `web/package-lock.json`, including dev/build dependencies. A tooling finding is not automatically a shipped-image vulnerability. It does not infer every opaque/minified/vendored JS library from image bytes.
- Checksum-pinned Trivy0.76.1 scans **local Docker images only**, with explicit Unix socket, `--image-src docker`, `--offline-scan`, `--scanners vuln`, `--pkg-types os,library`, and no remote image fallback or client/server/cloud mode. It inventories installed OS packages and recognized compiled language dependencies. It separately requires actual `usr/bin/neko` Go-binary inventory in the Neko image; missing coverage is UNSCANNED, never clear. CGO's dynamically linked X11/GTK/GStreamer libraries are covered by Debian package advisory matching, not Go source analysis.
- The vulnerability database is explicitly downloaded from the official Aqua GHCR database repository, then all four scans use `--skip-db-update` and record the exact local database and metadata SHA256. It is a time-varying advisory snapshot, not a forever-reproducible safety judgment.

Scanner commands use a dedicated empty workspace HOME/config, explicit workspace cache, sanitized environment and no inherited registry/npm credentials. Telemetry is disabled through both the supported `--disable-telemetry` flag and `TRIVY_DISABLE_TELEMETRY=true`; version announcements and VEX repository updates are disabled. No secret scanner is requested and raw file contents are not persisted. npm queries send dependency-version metadata to the official advisory endpoint; Go queries retrieve advisory metadata. Images and scan reports remain local.

`release-verify` is intentionally separate from ordinary deterministic `make check`: network authorization, advisory availability and native image inspection are explicit prerequisites. Download/permission/tool/DB/query failures remain UNAVAILABLE/NOT_RUN and produce a nonzero incomplete result. Findings remain preserved. Owned Go/npm findings, missing required OS/Neko-binary coverage, changed images, or any image HIGH/CRITICAL finding block the current release policy. Lower-severity image findings are still counted and retained for review; a policy PASS is not “vulnerability-free” or an exploitability claim. No blanket ignores or silent unfixed exclusions are applied.

The installer pins the explicit0.76.1 release and published official SHA256 before executing its binary; it uses no install script fetched for execution. Checksums/platform metadata are in `trivy-release.json`. Official release notes recommend0.76.1 for its Go1.27.3/CA mitigation. The affected historical0.69.4–0.69.6 artifacts are not used. Relevant primary sources: [release](https://github.com/aquasecurity/trivy/releases/tag/v0.76.1), [official March2026 incident discussion](https://github.com/aquasecurity/trivy/discussions/10425), [image scan documentation](https://trivy.dev/latest/docs/target/container_image/).

## Deterministic tool checks

The release verifier is itself included in owned formatting, vet and pinned Staticcheck. `make check` also runs its separate race-enabled test/coverage gate (80% minimum). Tests cover parser/redaction/bounds, isolated child environment, preserved failure reports, missing DB/tool/advisory cases, explicit-map validation, adapter and scanner identity mismatches, immutable-ID reinspection, tag-change detection and mocked orchestration. Its developer-tool statements are reported separately from production compute coverage; neither set of tests is removed from all checks. These mocked tests do not count as actual image/advisory verification.

After a complete scan, classify distinct HIGH/CRITICAL findings without changing raw artifacts:

```sh
go run ./scripts/compute/security -mode classify -output artifacts/compute/release/RUN_DIRECTORY
```

The classifier creates `high-critical-classification.json` once and refuses overwrite. It retains every observed occurrence of each selected ID (including lower-severity occurrences elsewhere), installed/fixed versions, vendor status, severity source and advisory URL. A missing fixed version is recorded as missing, not “unfixable.” Installed versions are observed affected-match versions; complete affected-version ranges require upstream/vendor review. The actual2026-10-30 scan and its concise summary remain under the timestamped local evidence directory; findings block release rather than silently becoming exceptions.
