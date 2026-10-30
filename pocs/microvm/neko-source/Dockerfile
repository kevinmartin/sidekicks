# Match the accepted POC's official Go build stage, then replace one source file.
FROM golang:1.25-trixie AS server
RUN apt-get update && apt-get install -y --no-install-recommends libx11-dev libxrandr-dev libxtst-dev libgtk-3-dev libxcvt-dev libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev && rm -rf /var/lib/apt/lists/*
COPY .cache/neko-v3.2.0.tar.gz /tmp/neko.tar.gz
RUN mkdir /src && tar xzf /tmp/neko.tar.gz --strip-components=2 -C /src neko-3.2.0/server
WORKDIR /src
COPY artifacts/loopback-manager.go /src/internal/webrtc/manager.go
RUN go build -mod=readonly -buildvcs=false -o /neko ./cmd/neko

# build.sh checks this local tag against the exact accepted image ID before and
# after building, and verifies that every base filesystem layer is preserved.
FROM sidekicks-neko:3.2.0-source
COPY --from=server /neko /usr/bin/neko
