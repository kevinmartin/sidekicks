SHELL := /bin/bash

BUILD_PATH	?= build
PRIVATE_KEY	?= $(BUILD_PATH)/private_key.pem
PUBLIC_KEY	?= $(BUILD_PATH)/public_key.pem
DEV_BUILD_PATH	:= $(BUILD_PATH)/dev
DEV_GATEWAY_PORT ?= 18080
DEV_FIXTURE_PORT ?= 18081
BROWSER_GATEWAY_URL ?= http://127.0.0.1:$(DEV_GATEWAY_PORT)
WBA_FIXTURE_URL ?= http://127.0.0.1:$(DEV_FIXTURE_PORT)
DEV_KUBECTL := kubectl --context docker-desktop
DEV_NAMESPACE := sidekicks-dev
DEV_MANIFESTS := infrastructure/kubernetes/dev
DEV_BROWSER_IMAGE := sidekicks-browser-use:latest
DEV_TOOLS_IMAGE := sidekicks-browser-dev-tools:latest
DEV_GATEWAY_TOKEN := $(DEV_BUILD_PATH)/gateway-token
DEV_TLS_KEY := $(DEV_BUILD_PATH)/tls.key
DEV_TLS_CERT := $(DEV_BUILD_PATH)/tls.crt
DEV_TLS_SPKI := $(DEV_BUILD_PATH)/fixture-spki
DEV_ORIGIN_SUFFIX := $(DEV_NAMESPACE).svc.cluster.local
DEV_YAML := $(addprefix $(DEV_BUILD_PATH)/,kustomization.yaml namespace.yaml deployment.yaml services.yaml)

.PHONY: keys
keys  : $(PRIVATE_KEY) $(PUBLIC_KEY)

.PHONY: clean
clean :
	@rm -rf $(BUILD_PATH)

.PHONY: test
test  :
	go test -v ./...

.PHONY     : integration
integration:
	@BROWSER_GATEWAY_URL="$(BROWSER_GATEWAY_URL)" WBA_FIXTURE_URL="$(WBA_FIXTURE_URL)" \
		BROWSER_GATEWAY_TOKEN="$${BROWSER_GATEWAY_TOKEN:-$$(cat "$(DEV_GATEWAY_TOKEN)" 2>/dev/null)}" \
		go test -v -count=1 -tags=integration ./...

.PHONY: dev
dev   :
	$(MAKE) container-build
	$(MAKE) kubernetes-apply
	$(MAKE) kubernetes-forward

.PHONY         : container-build
container-build:
	docker --context desktop-linux build --build-context sidekicks_src=. \
		-t "$(DEV_BROWSER_IMAGE)" infrastructure/images/browser-use
	docker --context desktop-linux build --build-context sidekicks_src=. \
		-t "$(DEV_TOOLS_IMAGE)" infrastructure/images/browser-dev-tools

.PHONY           : kubernetes-config
kubernetes-config: $(DEV_YAML) $(DEV_GATEWAY_TOKEN) $(DEV_TLS_SPKI)

.PHONY                : kubernetes-load-images
kubernetes-load-images:
	@set -eo pipefail; \
		docker --context desktop-linux image inspect "$(DEV_BROWSER_IMAGE)" "$(DEV_TOOLS_IMAGE)" >/dev/null || \
			{ echo "Run make container-build first." >&2; exit 1; }; \
		nodes=$$($(DEV_KUBECTL) get nodes -o jsonpath='{.items[*].metadata.name}'); \
		test -n "$$nodes"; \
		for node in $$nodes; do \
			kind_cluster=$$(docker --context desktop-linux inspect --type container \
				--format '{{index .Config.Labels "io.x-k8s.kind.cluster"}}' "$$node" 2>/dev/null || true); \
			if test -n "$$kind_cluster"; then \
				arch=$$($(DEV_KUBECTL) get node "$$node" -o jsonpath='{.status.nodeInfo.architecture}'); \
				docker --context desktop-linux image save --platform "linux/$$arch" \
					"$(DEV_BROWSER_IMAGE)" "$(DEV_TOOLS_IMAGE)" | \
					docker --context desktop-linux exec -i "$$node" ctr --namespace=k8s.io images import --platform "linux/$$arch" -; \
			fi; \
		done

.PHONY          : kubernetes-apply
kubernetes-apply: kubernetes-config kubernetes-load-images
	@set -e; \
		existing=$$($(DEV_KUBECTL) -n "$(DEV_NAMESPACE)" get deployment/browser-use --ignore-not-found -o name); \
		$(DEV_KUBECTL) apply -k "$(DEV_BUILD_PATH)"; \
		if test -n "$$existing"; then \
			$(DEV_KUBECTL) -n "$(DEV_NAMESPACE)" rollout restart deployment/browser-use; \
		fi
	$(DEV_KUBECTL) -n "$(DEV_NAMESPACE)" rollout status deployment/browser-use --timeout=120s

.PHONY            : kubernetes-forward
kubernetes-forward:
	$(DEV_KUBECTL) -n "$(DEV_NAMESPACE)" port-forward --address 127.0.0.1 deployment/browser-use \
		$(DEV_GATEWAY_PORT):8080 $(DEV_FIXTURE_PORT):8081

.PHONY         : kubernetes-down
kubernetes-down:
	$(DEV_KUBECTL) delete namespace "$(DEV_NAMESPACE)" --ignore-not-found --timeout=120s

# Helpers.

$(DEV_BUILD_PATH):
	@umask 077; mkdir -p "$@"

$(DEV_BUILD_PATH)/%.yaml: $(DEV_MANIFESTS)/%.yaml | $(DEV_BUILD_PATH)
	@cp "$<" "$@"

$(DEV_GATEWAY_TOKEN): | $(DEV_BUILD_PATH)
	@umask 077; token=$$(openssl rand -hex 32) && printf '%s' "$$token" > "$@"

$(DEV_TLS_KEY): | $(DEV_BUILD_PATH)
	@umask 077; openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:prime256v1 -out "$@.tmp" && mv "$@.tmp" "$@"

.PHONY       : check-dev-tls
check-dev-tls:

$(DEV_TLS_CERT): $(DEV_TLS_KEY) check-dev-tls
	@set -e; umask 077; \
		if ! test -s "$@" || test "$<" -nt "$@" || ! openssl x509 -in "$@" -noout -checkend 86400 >/dev/null 2>&1; then \
			openssl req -x509 -new -key "$<" -days 30 \
				-subj "/CN=signed.$(DEV_ORIGIN_SUFFIX)" \
				-addext "subjectAltName=DNS:signed.$(DEV_ORIGIN_SUFFIX),DNS:unsigned.$(DEV_ORIGIN_SUFFIX),DNS:blocked.$(DEV_ORIGIN_SUFFIX)" \
				-out "$@.tmp"; \
			mv "$@.tmp" "$@"; \
		fi

$(DEV_TLS_SPKI): $(DEV_TLS_CERT)
	@set -eo pipefail; umask 077; \
		openssl x509 -in "$<" -pubkey -noout | openssl pkey -pubin -outform DER | \
			openssl dgst -sha256 -binary | openssl base64 -A > "$@.tmp"; \
		mv "$@.tmp" "$@"

$(PRIVATE_KEY):
	@mkdir -p $(dir $@)
	@openssl genpkey -algorithm ed25519 -out $@
	@chmod 600 $@

$(PUBLIC_KEY): $(PRIVATE_KEY)
	@mkdir -p $(dir $@)
	@openssl pkey -in $< -pubout -out $@
