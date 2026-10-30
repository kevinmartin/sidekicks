FROM debian:bookworm-slim@sha256:7ff6ba31527b9e336162fa061746c366c4b2032f4d2cff2078bea43f78efe3f8 AS x11vnc-builder
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends build-essential autoconf automake libtool pkg-config ca-certificates curl libvncserver-dev libx11-dev libxtst-dev libxdamage-dev libxfixes-dev libxrandr-dev libxinerama-dev libxcomposite-dev libssl-dev zlib1g-dev libjpeg62-turbo-dev && rm -rf /var/lib/apt/lists/*
ARG X11VNC_SHA256=3ab47c042bc1c33f00c7e9273ab674665b85ab10592a8e0425589fe7f3eb1a69
RUN test -n "$X11VNC_SHA256" && curl -fsSL --retry 3 https://codeload.github.com/LibVNC/x11vnc/tar.gz/refs/tags/0.9.17 -o /tmp/x11vnc.tgz && echo "$X11VNC_SHA256  /tmp/x11vnc.tgz" | sha256sum -c - && mkdir /src && tar xzf /tmp/x11vnc.tgz --strip-components=1 -C /src && cd /src && autoreconf -fiv && ./configure --without-avahi --prefix=/usr/local && make -j2 && make install && sha256sum /tmp/x11vnc.tgz > /x11vnc-source.sha256

FROM debian:bookworm-slim@sha256:7ff6ba31527b9e336162fa061746c366c4b2032f4d2cff2078bea43f78efe3f8
ENV DEBIAN_FRONTEND=noninteractive DISPLAY=:99 PYTHONUNBUFFERED=1
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl python3 python3-aiohttp python3-pil xvfb x11vnc xdotool scrot openbox tint2 xterm chromium fonts-dejavu && rm -rf /var/lib/apt/lists/*
ARG NOVNC_VERSION=1.6.0
RUN curl -fsSL --retry 3 https://codeload.github.com/novnc/noVNC/tar.gz/refs/tags/v${NOVNC_VERSION} -o /tmp/novnc.tgz && echo '5066103959ef4e9b10f37e5a148627360dd8414e4cf8a7db92bdbd022e728aaa  /tmp/novnc.tgz' | sha256sum -c - && sha256sum /tmp/novnc.tgz > /novnc-source.sha256 && mkdir -p /opt/novnc && tar xzf /tmp/novnc.tgz --strip-components=1 -C /opt/novnc && rm /tmp/novnc.tgz
RUN apt-get update && apt-get install -y --no-install-recommends feh x11-xserver-utils && rm -rf /var/lib/apt/lists/*
RUN apt-get update && apt-get install -y --no-install-recommends picom pcmanfm thunar xfwm4 xfce4-panel xfdesktop4 xfce4-settings xfce4-terminal xfce4-session wmctrl dbus-x11 x11-utils && rm -rf /var/lib/apt/lists/*
COPY --from=x11vnc-builder /usr/local/bin/x11vnc /usr/local/bin/x11vnc
COPY --from=x11vnc-builder /x11vnc-source.sha256 /x11vnc-source.sha256
RUN /usr/local/bin/x11vnc -version > /x11vnc-version.txt 2>&1
RUN useradd -m -u 1000 desktop && mkdir -p /app /home/desktop/.config/openbox /home/desktop/.config/tint2 && chown -R desktop:desktop /app /home/desktop
COPY . /app
RUN test -f /app/desktop-assets/home.html && test -f /app/desktop-assets/wallpaper.png && test -f /app/desktop-assets/icons/files.png
RUN cp /app/openbox.xml /home/desktop/.config/openbox/rc.xml && cp /app/tint2rc /home/desktop/.config/tint2/tint2rc && cp -r /app/theme /home/desktop/.themes && mkdir -p /home/desktop/Sidekicks && cp /app/README.md /home/desktop/Sidekicks/POC-Report.md && chown -R desktop:desktop /home/desktop && chmod +x /app/entrypoint.sh && dpkg-query -W > /app/package-versions.txt
RUN sh /app/install-xfce-stage.sh
ENV LANG=C.UTF-8 LC_ALL=C.UTF-8 XCURSOR_THEME=Adwaita XCURSOR_SIZE=24 SIDEKICKS_DESKTOP_STACK=xfce
USER desktop
WORKDIR /app
EXPOSE 6080 8080
CMD ["/app/entrypoint-xfce.sh"]
