#!/bin/sh
# Prints the section of CHANGELOG.md for one version, without its heading,
# for the GitHub release. Fails when the version has no section or the
# section is empty, so no release goes out without notes.
# Usage: scripts/release-notes.sh <version>   (e.g. 0.1.0, without the v)
set -eu
cd "$(dirname "$0")/.."
version=${1:?usage: $0 <version>}
notes=$(awk -v v="$version" '
  /^## \[/ { if (found) exit; found = index($0, "## [" v "]") == 1; next }
  found
' CHANGELOG.md)
if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
  echo "CHANGELOG.md has no notes for $version" >&2
  exit 1
fi
printf '%s\n' "$notes" | sed -e '/./,$!d'
