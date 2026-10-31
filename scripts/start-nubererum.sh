#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
host=${HOST:-127.0.0.1}
port=${PORT:-4173}

if ! command -v go >/dev/null 2>&1; then
  printf '%s\n' "Go is required. Install Go 1.26 or newer and try again." >&2
  exit 1
fi

# The app finds bin/things-cli first. Build the patched CLI once; the plain
# release cannot read histories with Task2 items.
if [ ! -x "$root/bin/things-cli" ]; then
  "$root/scripts/build-things-cli.sh"
fi

cd "$root"

# Force interactive sign-in so Things credentials never enter shell history.
unset THINGS_USERNAME THINGS_EMAIL THINGS_PASSWORD THINGS_TOKEN THINGS_CLOUD_PASSWORD
export HOST="$host"
export PORT="$port"

if [ "$host" != "127.0.0.1" ] && [ "$host" != "localhost" ] && [ "${#APP_PASSWORD}" -lt 16 ]; then
  printf '%s\n' "HOST=$host is reachable from the network: set APP_PASSWORD (16+ characters) or use APP_AUTH=passkey." >&2
fi

printf '%s\n' "Building NubeRerum..."
go build -o nubererum ./cmd/nubererum

printf '%s\n' "NubeRerum is available at http://$host:$port"
printf '%s\n' "Enter your Things Cloud credentials in the browser. Press Ctrl+C to stop."
exec ./nubererum
