# One clean source recipe, four workload targets. Build from the repository root.
# No target inherits a historical/local Sidekicks image or runs its old helpers.
ARG DEBIAN_IMAGE=debian:trixie-slim@sha256:f6b08b09a05725f9431502d3b5714f5c4576e82c1a84fa1debe0c24a7e0c3ec4
ARG NEKO_GO_IMAGE=golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385

FROM ${DEBIAN_IMAGE} AS agent-base
ENV DEBIAN_FRONTEND=noninteractive LANG=C.UTF-8 LC_ALL=C.UTF-8
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd --gid 1000 sidekicks && \
    useradd --uid 1000 --gid 1000 --create-home --shell /bin/bash sidekicks && \
    mkdir -p /workspace && chown 1000:1000 /workspace
# Final targets copy the static Go agent only after dependency/source layers.
LABEL org.opencontainers.image.title="Sidekicks compute" \
      org.opencontainers.image.description="Go guest agent; isolation policy supplied by the compute runtime"
USER 1000:1000
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/sidekicks-agent"]
CMD []

FROM agent-base AS shell
COPY --chmod=0555 .run/compute-bin/sidekicks-agent /usr/local/bin/sidekicks-agent
ENV SIDEKICKS_ADAPTER=shell

# This workload intentionally has no X server, window manager, panel or desktop.
FROM agent-base AS browser-chromium
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium dbus-x11 fonts-dejavu-core fonts-noto-color-emoji && \
    rm -rf /var/lib/apt/lists/*
COPY --chmod=0555 .run/compute-bin/sidekicks-agent /usr/local/bin/sidekicks-agent
ENV SIDEKICKS_ADAPTER=browser-chromium
USER 1000:1000

# Both desktop variants share the same distro, GTK3, Xfce and application shell.
# The Go agent renders per-session settings and starts each owned component.
FROM agent-base AS desktop-base
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
    dbus dbus-x11 xvfb xfce4-session xfwm4 xfce4-panel xfdesktop4 \
    xfce4-settings xfce4-terminal thunar x11-utils x11-xserver-utils \
    xdg-utils adwaita-icon-theme librsvg2-common fonts-dejavu-core \
    fonts-noto-color-emoji && rm -rf /var/lib/apt/lists/*
COPY infrastructure/images/assets/desktop/wallpaper.png /usr/share/sidekicks/desktop/wallpaper.png
COPY infrastructure/images/assets/desktop/icons/*.png /usr/share/sidekicks/desktop/icons/
COPY infrastructure/images/assets/desktop/polish/settings.ini infrastructure/images/assets/desktop/polish/gtk.css /usr/share/sidekicks/desktop/polish/
COPY infrastructure/images/assets/desktop/polish/icons/ /usr/share/sidekicks/desktop/polish/icons/
USER 1000:1000

# Build-only download of official noVNC browser ES-module source. The npm
# package is CommonJS and is deliberately not used for direct browser imports.
FROM agent-base AS novnc-source
USER root
RUN apt-get update && apt-get install -y --no-install-recommends curl && \
    rm -rf /var/lib/apt/lists/*
RUN curl --fail --show-error --location --proto '=https' --tlsv1.2 \
      https://codeload.github.com/novnc/noVNC/tar.gz/refs/tags/v1.6.0 -o /tmp/novnc.tgz && \
    echo '5066103959ef4e9b10f37e5a148627360dd8414e4cf8a7db92bdbd022e728aaa  /tmp/novnc.tgz' | sha256sum -c - && \
    mkdir -p /opt/novnc && \
    tar --extract --gzip --file=/tmp/novnc.tgz --strip-components=1 --no-same-owner --directory=/opt/novnc && \
    test -f /opt/novnc/core/rfb.js && test -f /opt/novnc/LICENSE.txt && \
    rm /tmp/novnc.tgz

FROM desktop-base AS desktop-novnc
USER root
RUN apt-get update && apt-get install -y --no-install-recommends chromium x11vnc && \
    rm -rf /var/lib/apt/lists/* && \
    ln -s /usr/bin/x11vnc /usr/local/bin/x11vnc
COPY --from=novnc-source /opt/novnc/ /opt/novnc/
COPY --chmod=0555 .run/compute-bin/sidekicks-agent /usr/local/bin/sidekicks-agent
# The Go agent invokes x11vnc with -nowireframe and binds raw RFB to loopback.
ENV SIDEKICKS_ADAPTER=desktop-novnc
USER 1000:1000

# Build third-party Neko in its native Go/CGO toolchain. Native X11/GStreamer
# development packages stay in this stage, never in the runtime desktop.
FROM ${NEKO_GO_IMAGE} AS neko-source
USER root
ENV DEBIAN_FRONTEND=noninteractive GOTOOLCHAIN=local CGO_ENABLED=1
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl libx11-dev libxrandr-dev libxtst-dev libgtk-3-dev \
    libxcvt-dev libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev && \
    rm -rf /var/lib/apt/lists/*
RUN curl --fail --show-error --location --proto '=https' --tlsv1.2 \
      https://codeload.github.com/m1k1o/neko/tar.gz/refs/tags/v3.2.0 -o /tmp/neko.tar.gz && \
    echo '3e8a3f7d7f19ed1097b0c28b985894df6c40769ceb6d65cfdffea844026f839d  /tmp/neko.tar.gz' | sha256sum -c - && \
    mkdir -p /src && \
    tar --extract --gzip --file=/tmp/neko.tar.gz --strip-components=1 --no-same-owner --directory=/src && \
    rm /tmp/neko.tar.gz
WORKDIR /src/server
RUN echo 'a9102a6a216e149cd2a02a2eba5c6416976bdb2db421c5055c7142c05b7c3dfb  go.mod' | sha256sum -c - && \
    echo '1b0b1eee88525b492fb4430a17d1eb632e2bcc2b2d60fbab1f00a0a3ef7b260c  go.sum' | sha256sum -c -
COPY infrastructure/images/desktop-neko/upstream-modules/go.mod infrastructure/images/desktop-neko/upstream-modules/go.sum ./
RUN echo 'f5a599e3fa7f89516c099f8417f4f49a18c6f45f4e603765bc14eb4bfb6f63e0  go.mod' | sha256sum -c - && \
    echo '1082f5aad1bbb5649adccd7035b89ac7f5340c4d68accaaad8e12da83aa87d70  go.sum' | sha256sum -c - && \
    go build -mod=readonly -buildvcs=false -trimpath -o /out/neko ./cmd/neko

FROM desktop-base AS desktop-neko
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
    firefox-esr pulseaudio libx11-6 libxrandr2 libxtst6 libgtk-3-0t64 libxcvt0 \
    libgstreamer1.0-0 libgstreamer-plugins-base1.0-0 \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-pulseaudio && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /usr/lib/firefox-esr/distribution /usr/lib/firefox-esr/defaults/pref /usr/share/doc/neko
COPY --from=neko-source /out/neko /usr/bin/neko
COPY --from=neko-source /src/LICENSE /usr/share/doc/neko/LICENSE
COPY --from=neko-source /src/LICENSE /usr/share/doc/neko/SDK-LICENSE
COPY --from=neko-source /src/server/go.mod /src/server/go.sum /usr/share/doc/neko/
COPY infrastructure/images/desktop-neko/upstream-modules/manifest.json /usr/share/doc/neko/module-overlay.json
COPY infrastructure/images/clean/firefox/policies.json /usr/lib/firefox-esr/distribution/policies.json
COPY infrastructure/images/clean/firefox/autoconfig.js /usr/lib/firefox-esr/defaults/pref/sidekicks-autoconfig.js
COPY infrastructure/images/clean/firefox/sidekicks.cfg /usr/lib/firefox-esr/sidekicks.cfg
COPY infrastructure/images/clean/pulse-client.conf /etc/pulse/client.conf
COPY .run/compute-web/neko.js /usr/share/sidekicks/viewers/neko.js
COPY --chmod=0555 .run/compute-bin/sidekicks-agent /usr/local/bin/sidekicks-agent
# No fixed Neko password/config or Node/Python gateway is copied. The Go agent
# creates private per-instance Neko/pulse config, starts them, and gates control.
ENV SIDEKICKS_ADAPTER=desktop-neko
USER 1000:1000
