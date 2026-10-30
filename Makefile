SHELL := /bin/bash
ROOT := $(CURDIR)
export GOCACHE := $(ROOT)/.run/compute-go-cache
export GOPATH := $(ROOT)/.run/go
export STATICCHECK_CACHE := $(ROOT)/.run/compute-staticcheck-cache
export GOFLAGS := -mod=readonly
export GOTOOLCHAIN := local
GO ?= go
NPM ?= npm
NODE ?= node
PACKAGES := ./internal/... ./cmd/... ./web/...
INTEGRATION_PACKAGES := ./test/integration/...
RELEASE_PACKAGES := ./scripts/compute/security
STATICCHECK_VERSION := v0.8.2
GOVULNCHECK_VERSION := v1.9.0
TOOLS := $(ROOT)/.run/compute-tools
COVERAGE_FLOOR := 80
IMAGE_ARCH ?= $(shell $(GO) env GOARCH)
export SIDEKICKS_IMAGE_ARCH := $(IMAGE_ARCH)
SIDEKICKS_DOCKER_SOCKET ?= $(if $(filter Darwin,$(shell uname -s)),$(HOME)/.docker/run/docker.sock,/var/run/docker.sock)
export SIDEKICKS_DOCKER_SOCKET
export SIDEKICKS_NEKO_BASELINE_IMAGE SIDEKICKS_NEKO_CANDIDATE_IMAGE SIDEKICKS_APPS_IMAGE_ID SIDEKICKS_APPS_TEST_BROWSER
export SIDEKICKS_TEST_SHELL_IMAGE SIDEKICKS_TEST_BROWSER_IMAGE

.NOTPARALLEL: verify integration e2e images images-candidate
.PHONY: release-tools release-manifest release-verify release-tool-check help setup tools tools-check web-deps web-check web-build fmt fmt-check lint test test-race coverage coverage-check compile check build integration integration-browser integration-candidate apps-test-binary integration-apps e2e e2e-neko-audio verify-neko-candidate verify images images-candidate vuln clean
help:
	@printf '%s\n' 'make setup        install lockfile web dependencies + pinned Go analysis tools' 'make check        owned formatting, web Prettier, vet, Staticcheck, race tests, coverage gate, Linux compile matrix' 'make build        host CLI + static Linux agent for IMAGE_ARCH (arm64/amd64)' 'make images       build all four clean compute images; no POC replacement' 'make integration  actual Docker shell/browser functional tests; requires built images' 'make e2e          actual decoded noVNC/Neko viewer tests; requires clean images + Chrome' 'make verify       unique candidates + bound integration, app lifecycle, desktop and audio tests; no promotion' 'make vuln         pinned govulncheck against current advisory database (network required)' 'make release-tools install checksum-pinned local Trivy scanner only' 'make release-manifest record current immutable image/source/build-input metadata' 'make release-verify explicit online Go/npm +local immutable-image OS/native/Go scan; no builds/publish' 'make coverage     current per-function and policy coverage' 'make fmt          format owned Go and browser assets' 'make clean        remove only named compute binary outputs; retain evidence and POCs'
	@printf '%s\n' 'make images-candidate  build four uniquely tagged candidate images without retagging current images' 'make integration-candidate  run shell and browser integration against two explicit immutable IDs' 'make integration-apps   rebuild and test isolated apps against an exact image ID' 'make e2e-neko-audio     compare exact baseline/candidate images with real Opus audio' 'make verify-neko-candidate  deterministic checks + both apps tests + media comparison; full release gates remain separate'
setup: web-deps tools
web-deps:
	$(NPM) ci --prefix web --ignore-scripts --no-audit --no-fund
tools:
	@mkdir -p "$(TOOLS)"
	GOBIN="$(TOOLS)" $(GO) install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
	GOBIN="$(TOOLS)" $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
tools-check:
	@test -x "$(TOOLS)/staticcheck" || { echo 'Missing Staticcheck; run make tools' >&2; exit 1; }
	@$(GO) version -m "$(TOOLS)/staticcheck" | awk '$$1 == "mod" && $$2 == "honnef.co/go/tools" && $$3 == "$(STATICCHECK_VERSION)" { found=1 } END { if (!found) { print "Wrong Staticcheck version; run make tools" > "/dev/stderr"; exit 1 } }'
fmt:
	@gofmt -w $$(find cmd internal test web scripts/compute/security -name '*.go' -type f -not -path '*/node_modules/*')
	$(NPM) --prefix web run format
	web/node_modules/.bin/prettier --write 'test/e2e/*.mjs'
	web/node_modules/.bin/prettier --write scripts/compute/verify.mjs
fmt-check:
	@files="$$(gofmt -l $$(find cmd internal test web scripts/compute/security -name '*.go' -type f -not -path '*/node_modules/*'))"; test -z "$$files" || { printf 'Unformatted Go files:\n%s\n' "$$files"; exit 1; }
web-check:
	@test -x web/node_modules/.bin/prettier || { echo 'Missing locked web dependencies; run make web-deps' >&2; exit 1; }
	$(NPM) --prefix web run check
	web/node_modules/.bin/prettier --check 'test/e2e/*.mjs'
	web/node_modules/.bin/prettier --check scripts/compute/verify.mjs
	$(NODE) --check scripts/compute/verify.mjs
	@for file in test/e2e/*.mjs; do $(NODE) --check "$$file" || exit $$?; done
web-build:
	@test -x web/node_modules/.bin/esbuild || { echo 'Missing locked web dependencies; run make web-deps' >&2; exit 1; }
	$(NPM) --prefix web run build
lint: fmt-check tools-check
	$(GO) vet $(PACKAGES) $(RELEASE_PACKAGES) $(INTEGRATION_PACKAGES)
	"$(TOOLS)/staticcheck" $(PACKAGES) $(RELEASE_PACKAGES) $(INTEGRATION_PACKAGES)
test:
	$(GO) test -count=1 $(PACKAGES) $(RELEASE_PACKAGES)
	SIDEKICKS_DOCKER_INTEGRATION=0 SIDEKICKS_APPS_LIFECYCLE=0 SIDEKICKS_TEST_SHELL_IMAGE= SIDEKICKS_TEST_BROWSER_IMAGE= $(GO) test -count=1 $(INTEGRATION_PACKAGES)
test-race:
	@mkdir -p artifacts/compute
	$(GO) test -race -count=1 -covermode=atomic -coverprofile=artifacts/compute/coverage.out $(PACKAGES)
	SIDEKICKS_DOCKER_INTEGRATION=0 SIDEKICKS_APPS_LIFECYCLE=0 SIDEKICKS_TEST_SHELL_IMAGE= SIDEKICKS_TEST_BROWSER_IMAGE= $(GO) test -race -count=1 $(INTEGRATION_PACKAGES)
coverage-check: test-race
	bash scripts/compute/check-coverage.sh artifacts/compute/coverage.out $(COVERAGE_FLOOR)
coverage: coverage-check
	$(GO) tool cover -func=artifacts/compute/coverage.out
compile:
	@for arch in arm64 amd64; do \
	  echo "Compile production packages: linux/$$arch CGO_ENABLED=0"; \
	  CGO_ENABLED=0 GOOS=linux GOARCH="$$arch" $(GO) build $(PACKAGES) || exit $$?; \
	done
check: lint web-check coverage-check compile release-tool-check
build:
	@case "$(IMAGE_ARCH)" in arm64|amd64) ;; *) echo 'IMAGE_ARCH must be arm64 or amd64' >&2; exit 1 ;; esac
	@mkdir -p .run/compute-bin
	$(GO) build -trimpath -o .run/compute-bin/sidekicks ./cmd/sidekicks
	CGO_ENABLED=0 GOOS=linux GOARCH=$(IMAGE_ARCH) $(GO) build -trimpath -o .run/compute-bin/sidekicks-agent ./cmd/sidekicks-agent
images: build web-build
	bash scripts/compute/build-images.sh
images-candidate: build web-build
	bash scripts/compute/build-candidates.sh
integration: build
	SIDEKICKS_DOCKER_INTEGRATION=1 SIDEKICKS_APPS_LIFECYCLE=0 $(GO) test -count=1 -timeout=240s -v ./test/integration
integration-browser: build
	SIDEKICKS_DOCKER_INTEGRATION=1 SIDEKICKS_APPS_LIFECYCLE=0 $(GO) test -count=1 -timeout=120s -v ./test/integration -run '^TestDockerBrowserFunctional$$'
integration-candidate: build
	@[[ "$${SIDEKICKS_TEST_SHELL_IMAGE}" =~ ^sha256:[0-9a-f]{64}$$ && "$${SIDEKICKS_TEST_BROWSER_IMAGE}" =~ ^sha256:[0-9a-f]{64}$$ ]] || { echo 'Set exact SIDEKICKS_TEST_SHELL_IMAGE and SIDEKICKS_TEST_BROWSER_IMAGE IDs' >&2; exit 1; }
	SIDEKICKS_DOCKER_INTEGRATION=1 SIDEKICKS_APPS_LIFECYCLE=0 $(GO) test -count=1 -timeout=240s -v ./test/integration -run '^(TestDockerShellLifecycle|TestDockerBrowserFunctional)$$'
apps-test-binary:
	@case "$(IMAGE_ARCH)" in arm64|amd64) ;; *) echo 'IMAGE_ARCH must be arm64 or amd64' >&2; exit 1 ;; esac
	@mkdir -p .run/compute-bin
	CGO_ENABLED=0 GOOS=linux GOARCH=$(IMAGE_ARCH) $(GO) test -c -cover -trimpath -o .run/compute-bin/apps-lifecycle.test ./internal/compute/desktop/apps
integration-apps: apps-test-binary
	@test -n "$${SIDEKICKS_APPS_IMAGE_ID}" -a -n "$${SIDEKICKS_APPS_TEST_BROWSER}" || { echo 'Set exact SIDEKICKS_APPS_IMAGE_ID and SIDEKICKS_APPS_TEST_BROWSER' >&2; exit 1; }
	SIDEKICKS_APPS_LIFECYCLE=1 SIDEKICKS_APPS_TEST_BINARY="$(ROOT)/.run/compute-bin/apps-lifecycle.test" $(GO) test -count=1 -run '^TestIsolatedAppsLifecycle$$' -timeout=240s -v ./test/integration
e2e: build
	SIDEKICKS_E2E=1 $(NODE) test/e2e/desktop.mjs --execute
e2e-neko-audio: build
	@test -n "$${SIDEKICKS_NEKO_BASELINE_IMAGE}" -a -n "$${SIDEKICKS_NEKO_CANDIDATE_IMAGE}" || { echo 'Set exact SIDEKICKS_NEKO_BASELINE_IMAGE and SIDEKICKS_NEKO_CANDIDATE_IMAGE' >&2; exit 1; }
	SIDEKICKS_E2E=1 $(NODE) test/e2e/neko-audio.mjs --execute --baseline "$${SIDEKICKS_NEKO_BASELINE_IMAGE}" --candidate "$${SIDEKICKS_NEKO_CANDIDATE_IMAGE}"
# Scoped candidate acceptance; does not replace full verify or release-verify.
verify-neko-candidate:
	@[[ "$${SIDEKICKS_NEKO_BASELINE_IMAGE}" =~ ^sha256:[0-9a-f]{64}$$ && "$${SIDEKICKS_NEKO_CANDIDATE_IMAGE}" =~ ^sha256:[0-9a-f]{64}$$ && "$${SIDEKICKS_NEKO_BASELINE_IMAGE}" != "$${SIDEKICKS_NEKO_CANDIDATE_IMAGE}" ]] || { echo 'Set distinct exact baseline and candidate image IDs' >&2; exit 1; }
	$(MAKE) check
	SIDEKICKS_APPS_TEST_BROWSER=firefox SIDEKICKS_APPS_IMAGE_ID="$${SIDEKICKS_NEKO_BASELINE_IMAGE}" $(MAKE) integration-apps
	SIDEKICKS_APPS_TEST_BROWSER=firefox SIDEKICKS_APPS_IMAGE_ID="$${SIDEKICKS_NEKO_CANDIDATE_IMAGE}" $(MAKE) integration-apps
	$(MAKE) e2e-neko-audio
verify:
	$(NODE) scripts/compute/verify.mjs
vuln:
	@test -x "$(TOOLS)/govulncheck" || { echo 'Missing govulncheck; run make tools' >&2; exit 1; }
	@$(GO) version -m "$(TOOLS)/govulncheck" | awk '$$1 == "mod" && $$2 == "golang.org/x/vuln" && $$3 == "$(GOVULNCHECK_VERSION)" { found=1 } END { if (!found) { print "Wrong govulncheck version; run make tools" > "/dev/stderr"; exit 1 } }'
	"$(TOOLS)/govulncheck" $(PACKAGES)
clean:
	rm -f .run/compute-bin/sidekicks .run/compute-bin/sidekicks-agent

# Network-dependent advisory checks are intentionally not part of make check.
release-tools:
	bash scripts/compute/security/install-trivy.sh
release-tool-check:
	@mkdir -p artifacts/compute
	$(GO) test -race -count=1 -covermode=atomic -coverprofile=artifacts/compute/release-tool-coverage.out $(RELEASE_PACKAGES)
	@awk 'NR > 1 { total += $$2; if ($$3 > 0) covered += $$2 } END { if (!total) exit 1; score=100*covered/total; printf "Release verifier coverage: %.2f%% (required80%%)\n",score; if(score<80) exit 1 }' artifacts/compute/release-tool-coverage.out
release-manifest:
	$(GO) run $(RELEASE_PACKAGES) -mode manifest -socket "$(SIDEKICKS_DOCKER_SOCKET)"
release-verify:
	$(GO) run $(RELEASE_PACKAGES) -mode verify -online -socket "$(SIDEKICKS_DOCKER_SOCKET)"
