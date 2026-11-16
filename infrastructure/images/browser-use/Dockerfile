# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:25-bookworm-slim AS extension-build
WORKDIR /build
COPY extensions/sidekicks/package.json /extensions/sidekicks/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY extensions/sidekicks/manifest.json /extensions/sidekicks/tsconfig.json ./
COPY extensions/sidekicks/src/ ./src/
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.4-bookworm AS go-build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod /go.sum ./
COPY cmd/wba-native-host/ ./cmd/wba-native-host/
COPY cmd/sidekicks-guest/ ./cmd/sidekicks-guest/
COPY internal/services/browsergateway/ ./internal/services/browsergateway/
COPY internal/lib/browserapi/ ./internal/lib/browserapi/
COPY infrastructure/images/browser-use/export-stagehand/ ./export-stagehand/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go run ./export-stagehand /out/stagehand && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -o /out/ ./cmd/wba-native-host ./cmd/sidekicks-guest

FROM debian:trixie-slim AS browser-use

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates chromium fonts-liberation fonts-noto-color-emoji \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 2000 browser \
    && useradd --uid 2000 --gid 2000 --create-home --shell /usr/sbin/nologin browser \
    && chmod 0700 /home/browser

RUN install -d -m 0755 /etc/chromium/policies/managed /etc/chromium/native-messaging-hosts
COPY --chmod=0444 infrastructure/images/browser-use/policies.json /etc/chromium/policies/managed/10-sidekicks.json
COPY --from=go-build /out/stagehand/ /opt/sidekicks/stagehand/
COPY --from=extension-build /build/dist/ /opt/sidekicks/extension/
COPY --from=go-build --chmod=0555 /out/wba-native-host /opt/sidekicks/wba-native-host
COPY --chmod=0444 infrastructure/images/browser-use/com.sidekicks.wba.json /etc/chromium/native-messaging-hosts/com.sidekicks.wba.json
RUN test -s /opt/sidekicks/stagehand/manifest.json

ENV HOME=/home/browser LANG=C.UTF-8 WBA_BROKER_TIMEOUT=1s
USER 2000:2000

COPY --from=go-build --chmod=0555 /out/sidekicks-guest /opt/sidekicks/sidekicks-guest

# The runtime must allow Chromium to create its unprivileged user-namespace sandbox.
EXPOSE 8080
ENTRYPOINT ["/opt/sidekicks/sidekicks-guest"]
