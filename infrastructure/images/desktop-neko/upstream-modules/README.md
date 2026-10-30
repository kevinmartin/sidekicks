# Neko v3.2.0 module security overlay

This directory contains exact replacement `go.mod` and `go.sum` inputs for the pinned Neko server source archive. It is not another Sidekicks module or a fork of upstream application source. The clean Dockerfile verifies the original and replacement hashes before building. The resulting image's media and input functional checks and its advisory scans are recorded in [compute status](../../../../docs/compute/status.md); the full verification and release policy still fail for the reasons documented there.

The original archive SHA256 is `3e8a3f7d7f19ed1097b0c28b985894df6c40769ceb6d65cfdffea844026f839d`. Verify that archive and the original module-file hashes in `manifest.json`, extract `server/`, then replace only its two module files. Build with Go1.27.3, `GOTOOLCHAIN=local`, and `-mod=readonly`; do not run a floating `go get` during an image build. Preserve upstream licensing. The final linked CGO binary still needs an image scan and actual media/input/lifecycle acceptance.

The explicit fixes are Chi5.4.0, STUN3.1.5, DTLS3.1.4, x/net0.62.0, x/crypto0.58.0 and x/text0.43.0. Their minimal-version-selection closure also advances pion/transport/v4 to4.0.2, x/sys to0.49.0 and x/time to0.14.0. The module Go directive advances1.25.0→1.26.0, compatible with the installed1.27.3 compiler. Pinning x/crypto0.56.0 with x/net0.62.0 is incompatible because that x/net version requires x/crypto0.58.0; the failed attempt is retained in the local audit evidence.

Chi 5.4.0 still includes its deprecated `RealIP` middleware. An upgrade does not make that middleware safe to enable. The generated Neko configuration explicitly sets `server.proxy` to false, and the desktop session passes a fixed child environment rather than inheriting a `NEKO_SERVER_PROXY` override. Enabling proxy mode requires a separate trusted-proxy design and verification.

## Evidence and limitations

Local evidence is under `artifacts/compute/neko-security/` (intentionally ignored): original extracted ELF, pinned govulncheck1.9.0 JSON, original source/module hashes, module diff, selected versions and complete test logs. The exact original ELF came from immutable image `sha256:47b73b908c66990b4923da23eb380c999dd4af72e54b7815dde770e449ed3905` through a never-started, removed task-owned container.

The binary scanner identified vulnerable symbols in x/net DNS parsing, Pion STUN/DTLS parsing and Chi RealIP. SSH, HTML/HTTP2 and PRECIS findings were mostly module-level rather than identified vulnerable symbols in this binary. Symbol presence alone does not prove a deployment-specific untrusted-input call path. The unmaintained x/crypto/openpgp advisory has no upstream fix, but its vulnerable package/symbol was not identified in the binary; it must not be described as a tested exploit or blanket-excluded module.

Actual isolated source tests: STUN, Chi/router, Chi/middleware and Neko/pkg/utils passed with race detection. DTLS3.1.4 compiled but `TestHandshakeMessageCertificate` failed because its expected x509.Certificate fixture lacks the RawSignatureAlgorithm field populated by Go1.27; that failure remains a compatibility-test limitation, not a passing result. Neko/internal/config could not compile on the Mac because native GStreamer development pkg-config files are absent. No upstream Go source or test fixture was patched to conceal either result.

The initial module-only govulncheck command examined only the main module/stdlib, so its empty result is not a clean dependency scan. An attempted all-pattern module scan was rejected by the tool. A rebuilt exact ELF scan, full Linux CGO build/tests and real WebRTC VP8/Opus behavior remain required. No image was built or tagged by this audit worker.

## Chi5.4 follow-up

CVE-2026-114437 / [GHSA-j6qf-3m9c-vwj3](https://github.com/go-chi/chi/security/advisories/GHSA-j6qf-3m9c-vwj3) affects ClientIPFromXFF when it accepts an attacker-controlled forwarding chain without first verifying the direct peer. The patched release is5.4.0. The pinned Neko source does not reference that middleware; its separate deprecated RealIP path is disabled by the current server.proxy setting and must remain disabled. The previous exact binary symbol table contained RealIP/GetClientIP but not ClientIPFromXFF. These observations limit current reachability; they are not a blanket security guarantee.

This revision changes only the selected Chi version5.3.0→5.4.0 and its checksums relative to the prior overlay. Chi5.4 requires Go1.24, so the existing Go1.27.3 builder and overlay Go1.26 directive remain compatible. Both Chi/router and Chi/middleware race-short suites passed. Previous overlay files are preserved under artifacts/compute/neko-security/overlay-before-chi54/. No Dockerfile, image or live POC was modified by this worker. The resulting exact binary must be rescanned and exercised after the parent builds it.
