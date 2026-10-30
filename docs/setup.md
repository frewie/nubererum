# Setup & Commands

NubeRerum is one Go binary that runs `things-cli` and serves a static frontend from `public/`. This page covers local setup, Docker Compose and the environment variables the server reads. For deployment procedures, see [Deployment](DEPLOYMENT.md).

## Prerequisites

| Requirement | Version | Evidence |
| --- | --- | --- |
| Go | 1.26 (`go 1.26.4` in `go.mod`; `golang:1.26-alpine` in the `Dockerfile`) | `go.mod`, `Dockerfile` |
| Git | any, to build `things-cli` | `scripts/build-things-cli.sh` |
| `things-cli` | `arthursoares/things-cloud-sdk` v0.6.0 (commit checked) plus `patches/things-cloud-sdk-arthursoares.patch` | `Dockerfile` (`THINGS_SDK_VERSION`, `THINGS_SDK_COMMIT`) |
| Node.js | `>= 18`, for Playwright and the parity tools only | `package.json` |
| Docker with Compose | for the container deployment | `compose.yaml` |

The server needs the Things Cloud email and password to run `things-cli`. Things Cloud has no app passwords today.

The only Go module dependency is `github.com/go-webauthn/webauthn` (passkey mode).

## Building things-cli

The unpatched v0.6.0 release cannot read histories with `Task2` items and lacks the write operations the app uses. Build the patched CLI the same way the image does:

| Platform | Command | Result |
| --- | --- | --- |
| Local (Linux, macOS) | `scripts/build-things-cli.sh` | `bin/things-cli` |
| Docker | `docker compose up -d --build` | `/usr/local/bin/things-cli` in the image |

The script reads the SDK version and commit from the `Dockerfile`, clone the SDK, refuse to build when the tag points to another commit, apply the patch and build. They need network access to GitHub.

## Local run

```sh
./scripts/start-nubererum.sh
```

The script:

- builds `bin/things-cli` with `scripts/build-things-cli.sh` if it does not exist,
- unsets `THINGS_USERNAME`, `THINGS_EMAIL`, `THINGS_PASSWORD`, `THINGS_TOKEN` and `THINGS_CLOUD_PASSWORD`, so you sign in to Things Cloud in the browser,
- sets `HOST=127.0.0.1` and `PORT=4173` unless they are set,
- builds `./nubererum` and starts it.

On a network address such as `HOST=0.0.0.0` the server refuses to start without an `APP_PASSWORD` of at least 16 characters (`validateSecurityConfig` in `security.go`); the script warns about that first. Run without the script, the server also binds to `127.0.0.1`.

## Docker Compose

```sh
cp .env.example .env
# edit .env: APP_USERNAME, APP_PASSWORD, THINGS_USERNAME, THINGS_PASSWORD, TZ
docker compose up -d --build
```

`compose.yaml` sets:

| Variable in the container | Value |
| --- | --- |
| `HOST` | `0.0.0.0` |
| `PORT` | `4173` |
| `TZ` | `${TZ:-Europe/Berlin}` |
| `DATA_DIR` | `/data` |
| `THINGS_CLI_CACHE` | `/data/things-cli-state-v8.json` |
| `APP_AUTH` | `${APP_AUTH:-basic}` |
| `APP_USERNAME` | `${APP_USERNAME:-admin}` |
| `APP_PASSWORD` | required in basic mode (the default): the app refuses to start without one of at least 16 characters; passkey mode leaves it out |
| `THINGS_USERNAME`, `THINGS_PASSWORD` | required; Compose refuses to start without them |
| `APP_PUBLIC_ORIGINS` | optional, comma separated |
| `APP_PASSKEY_ORIGINS` | optional, comma separated (passkey mode; `APP_PUBLIC_ORIGINS` when unset) |

The container is read-only with a small `/tmp` tmpfs, drops all capabilities, sets `no-new-privileges`, and limits processes (128), memory (512 MB) and logs (3 × 10 MB). It publishes port `4173`.

The `Dockerfile` has two stages. The `golang:1.26-alpine` stage builds the app, clones `arthursoares/things-cloud-sdk` at `v0.6.0`, checks the commit, applies the patch and builds `things-cli`. The `alpine:3.22` stage adds `ca-certificates` and `tzdata`, a non-root user `app` (uid `10001`), both binaries in `/usr/local/bin` and `public/`.

For passkey sign-in, set `APP_AUTH=passkey` and `APP_PUBLIC_ORIGINS` (or `APP_PASSKEY_ORIGINS`) in `.env`; `APP_PASSWORD` can be left out (see [Deployment — Passkey Sign-In](DEPLOYMENT.md#passkey-sign-in)).

### Data volume

Compose mounts the named volume `things-data` at `/data`. It holds:

- `things-cli-state-v8.json`, the CLI state cache (task metadata of the whole account),
- `passkeys.json` and `passkey-setup.json` in passkey mode.
- `mcp-tokens.json` when MCP tokens exist.

Treat all of them as private.

## Environment variables

The server reads only environment variables; there is no config file.

| Variable | Default | Read in | Notes |
| --- | --- | --- | --- |
| `PORT` | `4173` | `main()` | Listen port. |
| `HOST` | `127.0.0.1` | `main()` | Listen address. |
| `APP_AUTH` | `basic` | `authMode()` | `basic` or `passkey`; anything else stops the start. |
| `APP_USERNAME` | `admin` | `basicAuth` | Basic Auth user. In passkey mode, the WebAuthn user name. |
| `APP_PASSWORD` | unset | `basicAuth`, `validateSecurityConfig` | Enables Basic Auth. Required off loopback in basic mode, at least 16 characters. Ignored in passkey mode. |
| `APP_PUBLIC_ORIGINS` | unset | `sameOriginGuard`, `passkeyOrigins` | Extra origins for the same-origin guard (proxies that rewrite `Host`). Also the passkey origins when `APP_PASSKEY_ORIGINS` is unset. |
| `APP_PASSKEY_ORIGINS` (or `APP_PASSKEY_ORIGIN`) | unset | `passkeyOrigins` | Passkey origins, comma separated. `https` only, except `http://localhost`. |
| `APP_READ_ONLY` | unset | `readOnly()` | `1`, `true` or `yes` rejects all write endpoints with `403`. |
| `THINGS_USERNAME` | unset | `session` | Things Cloud email. Fallback `THINGS_EMAIL`. |
| `THINGS_PASSWORD` | unset | `session` | Things Cloud password. Fallbacks `THINGS_TOKEN`, `THINGS_CLOUD_PASSWORD`. |
| `DATA_DIR` | app root | `cachePath`, `passkeyDataDir` | Directory for the cache and the passkey files. |
| `THINGS_CLI_CACHE` | `DATA_DIR/things-cli-state-v8.json` | `cachePath` | Path of the CLI state cache. |
| `THINGS_CLI_WORKER` | on | `workerEnabled` | `0` runs one `things-cli` process per command instead of the long-running `things-cli serve` worker. |
| `THINGS_CLOUD_CLI` | unset | `cliPath` | Path of the `things-cli` binary. |
| `TZ` | system | Go and `things-cli` | Decides which day is "today". Set it to the time zone of your Things devices. |

When the Things account comes from the environment (`THINGS_USERNAME` and `THINGS_PASSWORD` both set), the account is "server managed": `POST /api/login` and `POST /api/logout` return `403`, the Settings (disconnect) button is hidden, and the server syncs once at start.

### things-cli discovery

`cliPath()` in `main.go` tries, in order:

1. `THINGS_CLOUD_CLI`
2. `<app root>/bin/things-cli`
3. `/usr/local/bin/things-cli` (Docker)
4. `things-cli` on `PATH`

The app root is the directory of the executable if it contains `public/index.html`, else the working directory. Without a CLI, every route that needs it returns `424` with "things-cli is missing. Install it or set THINGS_CLOUD_CLI."

### State cache

`cachePath()` returns `THINGS_CLI_CACHE`, else `DATA_DIR/things-cli-state-v8.json`, else `<app root>/things-cli-state-v8.json`, and creates the directory with mode `0700`. `things-cli` writes the file; the server only reads it. When the CLI state format changes, the file name changes (`-vN`), so old caches are rebuilt.

## Passkey commands

In passkey mode, run these inside the container (or next to the binary with the same `DATA_DIR`):

| Task | Command |
| --- | --- |
| Create a one-time setup code (30 minutes) | `docker exec nubererum nubererum passkey-setup` |
| List passkeys | `docker exec nubererum nubererum passkey-list` |
| Remove a passkey | `docker exec nubererum nubererum passkey-remove <id or first 6+ characters>` |
| Create an MCP token for a coding agent | `docker exec nubererum nubererum mcp-token-create --name <name> --project <uuid> [--read-only]` |
| Show project UUIDs for MCP tokens | `docker exec nubererum nubererum mcp-token-projects` |
| List or remove MCP tokens | `docker exec nubererum nubererum mcp-token-list` / `mcp-token-remove <name>` |

## Commands at a glance

| Task | Command |
| --- | --- |
| Build the app | `go build -o nubererum ./cmd/nubererum` |
| Build things-cli without Docker | `scripts/build-things-cli.sh` |
| Run locally without Docker | `./scripts/start-nubererum.sh` |
| Docker up | `docker compose up -d --build` |
| Docker down | `docker compose down` |
| Docker logs | `docker compose logs -f nubererum` |
| All checks (Go in a container, parity, Playwright; `--race` adds the race run) | `scripts/check.sh [--race]` |
| Go vet and tests | `go vet ./... && go test ./...` |
| Go checks without local Go | `docker run --rm -v "$PWD":/src -w /src golang:1.26-alpine sh -c 'gofmt -l . ; go vet ./... && go test -count=1 ./...'` |
| Browser test dependencies | `npm install && npx playwright install chromium` |
| Browser tests | `npm run test:e2e` |
| Parity unit tests | `npm run test:parity` |
| Parity comparison | `npm run compare:parity -- --reference .things-parity/reference.json` |

## Troubleshooting

- **The app exits at start with "APP_PASSWORD is required …" or "… at least 16 characters"**: `HOST` is not loopback in basic mode. Set a long `APP_PASSWORD`, use `HOST=127.0.0.1`, or use passkey mode.
- **The app exits with "APP_AUTH=passkey needs APP_PASSKEY_ORIGINS or APP_PUBLIC_ORIGINS"**: set an `https://` origin.
- **424 "things-cli is missing"**: build the CLI or set `THINGS_CLOUD_CLI`.
- **401 "Sign in with your Things Cloud email and password first."**: no account in the environment and no browser sign-in yet.
- **401 "Sign in with your passkey."**: passkey mode without a session; open `/login`.
- **403 "This instance is read-only (APP_READ_ONLY)."**: expected on read-only instances.
- **409 "… changed. Refresh and try again."**: the server's model no longer has the to-do, project, area or tag. Refresh.
- **502**: `things-cli` failed; the body carries its trimmed error output. Check that the host reaches Things Cloud: `docker compose exec nubererum /usr/local/bin/things-cli today`.
- **The first sync is slow**: the CLI replays the account history once; later syncs are incremental.
