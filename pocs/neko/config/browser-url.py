#!/usr/bin/python3
"""Guest default-browser handler; desktop API launchers still take no URL."""
from pathlib import Path
import subprocess
import sys
from urllib.parse import urlsplit


def browser_command(arguments):
    if not arguments:
        return ["/etc/sidekicks/browser.sh"]
    if len(arguments) != 1:
        raise ValueError("one browser URL is required")
    url = arguments[0]
    if any(ord(c) < 32 for c in url):
        raise ValueError("browser URL cannot contain control characters")
    # GIO's %u expansion may pass a local absolute filename rather than a URI.
    # This handler runs inside the guest; convert that one filename to file:.
    if url.startswith('/') and not url.startswith('//'):
        url = Path(url).as_uri()
    parsed = urlsplit(url)
    if parsed.scheme not in {"http", "https", "file"}:
        raise ValueError("choose a guest http, https or file URL")
    if parsed.scheme in {"http", "https"} and not parsed.netloc:
        raise ValueError("HTTP URL requires a host")
    if parsed.scheme == "file" and (parsed.netloc not in {"", "localhost"} or not parsed.path.startswith("/")):
        raise ValueError("file URL must be local to this guest")
    profile = Path("/tmp/sidekicks-firefox-profile-path").read_text().strip()
    return ["firefox-esr", "--profile", profile, "--class", "SidekicksBrowser", "--new-tab", url]


if __name__ == "__main__":
    try:
        command = browser_command(sys.argv[1:])
    except ValueError as error:
        raise SystemExit(str(error))
    raise SystemExit(subprocess.call(command))
