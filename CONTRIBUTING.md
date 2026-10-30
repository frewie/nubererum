# Contributing

Thanks for helping. Small, focused changes with tests are easiest to review.

## Setup

- Go 1.26 (or Docker: the check script runs Go in a container), Node.js for the browser tests.
- `scripts/build-things-cli.sh` builds the patched `things-cli` into `bin/`.
- `scripts/start-nubererum.sh` starts the app on `127.0.0.1:4173`. See [`docs/setup.md`](docs/setup.md).

## Checks

Run every suite before you open a pull request:

```sh
npm install && npx playwright install chromium
scripts/check.sh --race
```

It runs gofmt, `go vet` and the Go tests (plus the race detector), the parity unit tests and the Playwright tests, and prints `go: ok`, `race: ok`, `parity: ok` and the browser test count.

## Rules

- **No real Things account in tests.** The Go tests stub `things-cli`, and the Playwright tests mock the API. Never commit credentials, `.env`, `things-cli-state*.json`, `passkeys.json`, `passkey-setup.json` or `bin/`.
- **Writes copy what Things itself writes.** There is no official API: every new write mirrors a payload seen in a real Things history and is covered by a wire test in the SDK patch. Do not guess payloads; a malformed write can disturb every Things device on the account.
- Keep the invariants listed in [`AGENTS.md`](AGENTS.md) (Today membership, the visible filter, the Logbook rule, one write at a time) and the security controls in [`docs/SECURITY.md`](docs/SECURITY.md).
- New write endpoints go into `writePaths` (`cmd/nubererum/edits.go`) so `APP_READ_ONLY=1` blocks them.
- Update the docs that describe your change (`docs/api.md`, `docs/architecture.md`, `docs/limitations.md`, `docs/testing.md`).

## Changing the SDK patch

The image builds `things-cli` from `arthursoares/things-cloud-sdk` v0.6.0 plus `patches/things-cloud-sdk-arthursoares.patch`:

```sh
git clone -q --depth 1 --branch v0.6.0 https://github.com/arthursoares/things-cloud-sdk.git sdk
cd sdk && git apply ../nubererum/patches/things-cloud-sdk-arthursoares.patch
# edit and test
docker run --rm -v "$PWD":/src -w /src golang:1.26-alpine sh -c 'gofmt -l .; go vet ./... && go test ./...'
git add -A && git diff --cached > ../nubererum/patches/things-cloud-sdk-arthursoares.patch
```

`docker build .` proves that the patch applies.

## License

By contributing you agree that your contribution is licensed under the MIT License (see `LICENSE`).
