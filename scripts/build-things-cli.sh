#!/bin/sh
# Builds the patched things-cli into bin/things-cli, like the Docker image:
# the original SDK at the release and commit pinned in the Dockerfile, plus
# patches/things-cloud-sdk-arthursoares.patch.

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=$(sed -n 's/^ARG THINGS_SDK_VERSION=//p' "$root/Dockerfile")
commit=$(sed -n 's/^ARG THINGS_SDK_COMMIT=//p' "$root/Dockerfile")
if [ -z "$version" ] || [ -z "$commit" ]; then
  printf '%s\n' "Could not read THINGS_SDK_VERSION and THINGS_SDK_COMMIT from the Dockerfile." >&2
  exit 1
fi
for tool in git go; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf '%s\n' "$tool is required to build things-cli." >&2
    exit 1
  fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

printf '%s\n' "Building things-cli $version with the app's patch..."
git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$version" https://github.com/arthursoares/things-cloud-sdk.git "$work/sdk"
if [ "$(git -C "$work/sdk" rev-parse HEAD)" != "$commit" ]; then
  printf '%s\n' "Tag $version does not point to the pinned commit $commit; refusing to build." >&2
  exit 1
fi
git -C "$work/sdk" apply "$root/patches/things-cloud-sdk-arthursoares.patch"
mkdir -p "$root/bin"
(cd "$work/sdk" && CGO_ENABLED=0 go build -trimpath -o "$root/bin/things-cli" ./cmd/things-cli)
printf '%s\n' "Built $root/bin/things-cli"
