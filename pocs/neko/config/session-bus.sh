#!/bin/sh
set -eu
# This socket belongs only to this disposable guest and this unprivileged user.
rm -f /tmp/sidekicks-session-bus
exec /usr/bin/dbus-daemon --session --nofork --nopidfile --address=unix:path=/tmp/sidekicks-session-bus
