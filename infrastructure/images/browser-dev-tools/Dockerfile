# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.4-bookworm AS go-build
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH
WORKDIR /src
COPY --from=sidekicks_src /go.mod /go.sum ./
COPY --from=sidekicks_src /cmd/browser-gateway/ ./cmd/browser-gateway/
COPY --from=sidekicks_src /internal/browsergateway/ ./internal/browsergateway/
COPY --from=sidekicks_src /internal/webbotauth/ ./internal/webbotauth/
COPY wba-fixture/ ./wba-fixture/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -o /out/ ./cmd/browser-gateway ./wba-fixture

FROM scratch
COPY --from=go-build /out/ /
USER 2000:2000
