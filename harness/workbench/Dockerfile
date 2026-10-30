FROM scratch
COPY sidekicks-workbench /sidekicks-workbench
COPY shared/ /app/shared/
COPY pocs/ /app/pocs/
USER 65532:65532
EXPOSE 7790
ENTRYPOINT ["/sidekicks-workbench", "-root", "/app", "-listen", ":7790", "-novnc-url", "http://host.docker.internal:7791", "-neko-url", "http://host.docker.internal:7792"]
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD ["/sidekicks-workbench", "-healthcheck"]
