#!/bin/sh
# Runs every test suite of the app; exits non-zero on any failure.
# Usage: scripts/check.sh [--race]
#   --race  also runs the Go tests with the race detector.
set -e
cd "$(dirname "$0")/.."

race=0
for arg in "$@"; do
  case "$arg" in
    --race) race=1 ;;
    *) echo "usage: $0 [--race]" >&2; exit 2 ;;
  esac
done

# Go: gofmt, vet and tests in a container (Go need not be installed).
docker run --rm -v gomodcache:/go/pkg/mod -v "$PWD":/src -w /src golang:1.26-alpine sh -c \
  'test -z "$(gofmt -l .)" || { echo "gofmt:"; gofmt -l .; exit 1; }
   go vet ./... && go test -count=1 ./... >/tmp/go.log 2>&1 || { grep -v "level=\|auth:" /tmp/go.log; exit 1; }' \
  || { echo "go: FAILED"; exit 1; }
echo "go: ok"

if [ "$race" = 1 ]; then
  docker run --rm -v gomodcache:/go/pkg/mod -v "$PWD":/src -w /src -e CGO_ENABLED=1 golang:1.26 sh -c \
    'go test -race -count=1 ./... >/tmp/race.log 2>&1 || { grep -v "level=\|auth:" /tmp/race.log; exit 1; }' \
    || { echo "race: FAILED"; exit 1; }
  echo "race: ok"
fi

parity=$(npm run -s test:parity 2>&1) || { echo "$parity"; echo "parity: FAILED"; exit 1; }
echo "parity: ok"

# Playwright starts the app with `go run` (playwright.config.js).
out=$(npx playwright test 2>&1) || true
echo "$out" | grep -E "^\s+[0-9]+\) " || true
if echo "$out" | grep -qE "^\s+[0-9]+ failed" || ! echo "$out" | grep -qE "^\s+[0-9]+ passed"; then
  echo "$out" | tail -40
  echo "e2e: FAILED"
  exit 1
fi
echo "$out" | grep -E "^\s+[0-9]+ (passed|skipped)"
