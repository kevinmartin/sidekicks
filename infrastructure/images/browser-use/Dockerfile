# syntax=docker/dockerfile:1
ARG DEBIAN_IMAGE=debian:trixie-slim
ARG GO_IMAGE=golang:1.27.3-bookworm

FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS extension-build
WORKDIR /build
COPY --from=sidekicks_src /extensions/sidekicks/package.json /extensions/sidekicks/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY --from=sidekicks_src /extensions/sidekicks/manifest.json /extensions/sidekicks/tsconfig.json ./
COPY --from=sidekicks_src /extensions/sidekicks/src/ ./src/
RUN npm run build

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS go-build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY --from=sidekicks_src /go.mod /go.sum ./
COPY --from=sidekicks_src /cmd/wba-native-host/ ./cmd/wba-native-host/
COPY export-stagehand/ ./export-stagehand/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go run ./export-stagehand /out/stagehand && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -o /out/wba-native-host ./cmd/wba-native-host

FROM ${DEBIAN_IMAGE} AS browser-use

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates chromium fonts-liberation fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 2000 browser \
    && useradd --uid 2000 --gid 2000 --create-home --shell /usr/sbin/nologin browser \
    && chmod 0700 /home/browser

RUN install -d -m 0755 /etc/chromium/policies/managed /etc/chromium/native-messaging-hosts
COPY --chmod=0444 policies.json /etc/chromium/policies/managed/10-sidekicks.json
COPY --from=go-build /out/stagehand/ /opt/sidekicks/stagehand/
COPY --from=extension-build /build/dist/ /opt/sidekicks/extension/
COPY --from=go-build --chmod=0555 /out/wba-native-host /opt/sidekicks/wba-native-host
COPY --chmod=0444 com.sidekicks.wba.json /etc/chromium/native-messaging-hosts/com.sidekicks.wba.json
RUN test -s /opt/sidekicks/stagehand/manifest.json

ENV HOME=/home/browser LANG=C.UTF-8 \
    WBA_BROKER_SOCKET=/run/sidekicks/wba/broker.sock WBA_BROKER_TIMEOUT=1s
USER 2000:2000

# The runtime must allow Chromium's unprivileged user-namespace sandbox.
# Keep CDP on loopback and allow the Stagehand worker's specific WebSocket
# origin. Chromium derives both unpacked extension IDs from their paths.
ENTRYPOINT ["/usr/bin/chromium", \
            "--headless", "--disable-setuid-sandbox", "--no-first-run", \
            "--no-default-browser-check", "--window-size=1280,800", \
            "--user-data-dir=/home/browser/profile", \
            "--enable-unsafe-extension-debugging", \
            "--load-extension=/opt/sidekicks/stagehand,/opt/sidekicks/extension", \
            "--remote-allow-origins=chrome-extension://mibgiokgnjiohdeccdpmccjlfpdoliim"]
CMD ["--remote-debugging-address=127.0.0.1", "--remote-debugging-port=9222", "about:blank"]
