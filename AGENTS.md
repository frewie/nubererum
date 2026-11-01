# AGENTS.md

NubeRerum — repository guidance for agents.

## Repository Documentation

This block is managed by the repository-documenter. Do not edit the surrounding prose by hand; regenerate it from the sources below.

**Revision:** `bb3357bdc159e699f83f1ceb80c6245969d3b0ba`

### Authority hierarchy (highest wins on conflict)

1. **Source + tests** — `cmd/nubererum/*.go` (including `*_test.go`), `public/*`, `tests/*`, `playwright.config.js`, `compose.yaml`, `Dockerfile`, `.env.example`, `package.json`, `go.mod`, `scripts/*`, `patches/things-cloud-sdk-arthursoares.patch`.
2. **Operator docs** — `README.md`, `docs/DEPLOYMENT.md`, `docs/SECURITY.md`, `docs/PARITY_TESTING.md`.
3. **Generated summaries** — `mkdocs.yml`, `docs/index.md`, `docs/setup.md`, `docs/api.md`, `docs/architecture.md`, `docs/testing.md`, `docs/limitations.md`, `docs/repository-map.json`, `llms.txt`, `llms-full.txt`, and this `AGENTS.md` block. These are derived, secondary, and must be updated (not the source of truth).

### One-paragraph map

A self-hosted Go (1.26) web app to view and edit Things 3 tasks in Things Cloud. There is no official Things Cloud API, so the server (`cmd/nubererum/`) runs `things-cli` (from `arthursoares/things-cloud-sdk` v0.6.0 plus `patches/things-cloud-sdk-arthursoares.patch`) as a child process. Reads run `things-cli list` and `things-cli today` and build the model from the CLI state cache `things-cli-state-v8.json`; the server keeps the last model in memory (`modelcache.go`) and refreshes it in the background. Writes go through `things-cli batch` (and `edit`, `complete`, `create-tag`, `create-area`) one at a time with safe retries (`writes.go`). A dependency-free static frontend in `public/` mimics Things: lists, Logbook, Trash, sidebar, editor, selection, keyboard shortcuts (`public/shortcuts.js`), drag and drop, light and dark appearance (`public/theme.js`). Auth is Basic Auth (`APP_AUTH=basic`, default) or WebAuthn passkeys (`APP_AUTH=passkey`). The API is listed in `docs/api.md`.

### Invariants and non-obvious facts (verify before changing)

- **Today membership comes only from `things-cli today`.** Cache fields (`TodayIndex`, `TodayIndexReferenceDate`) affect order, never membership. Exception: completed to-dos not yet logged keep their place in their lists, Today included; completed and canceled projects stay in the sidebar the same way (`stillListedAs`) (logbook rule from the cached Things settings `ld`/`li`, `logbookRule` in `main.go`). Guarded by `TestApplyCLIToday*`, `TestCompletedTodayStaysInTodayAfterCLIToday`, `TestLogbookRuleKeepsTodaysCompletedUntilLogged`.
- **The visible filter:** the model keeps only to-dos `things-cli list` returns, plus completed ones not yet logged. Guarded by `TestApplyVisibleTaskFilterRemovesCacheOnlyRowsAndRecounts`, `TestSyncModelUsesThingsCLI`.
- **One sync at a time, one write at a time.** All commands go to one long-running `things-cli serve` process (`cliworker.go`), which runs them in order, keeps the Things Cloud connection and the state in memory; `THINGS_CLI_WORKER=0` or a CLI without serve mode falls back to one process per command. The worker keeps the SDK's write preflight index (`KeepPreflightIndex`), so writes do not re-read the whole history; `TestKeptPreflightIndexMatchesFullScan` (SDK patch) pins that it matches a full scan. Parallel CLI runs race on the cache file; parallel writes get `409 OutdatedAncestor`. Writes retry only after errors that prove nothing was written (never after timeouts). Cached models are never modified in place; patches copy them.
- **Repeating to-dos:** the SDK preflight (`checkTask7Targets`, patch) lets instances (`rt` names the template, `rr` null) take every verified change and templates (`rr` set, with `icsd`/`acrd`) only `md tt nt tr`; `rp`, or `icsd`/`acrd` without `rr`, stay refused. `withWriteRetry` turns the template refusal into a `409` with an explanation (`explainWriteError`). Guarded by `TestHistoryWriteRepeatingInstancesAndTemplates`, `TestCmdRepeatingTemplateWire` (patch) and `TestWriteRefusedOnRepeatingTemplateExplainsItself`. New templates (`POST /api/repeat`) carry only the `rr` shapes `validateRepeatRule` (patch) accepts: `rrv` 4, no end, no early start, one offset per unit, `icsd` a day, `st` 2, no start date; guarded by `TestValidateRepeatRule`, `TestCmdBatchCreateRepeatingWire` (patch) and `TestRepeatCreatesATemplateAndTrashesTheOriginal`.
- **`APP_READ_ONLY=1`** rejects every path in `writePaths` (`edits.go`) with `403`. Add new write endpoints to `writePaths`.
- **No reader without the CLI.** `backendInfo()` always returns `directCloud: true, nativeFallback: false`, but every CLI-backed route returns `424` without `things-cli`.
- **`GET /api/sync` exposes the full CLI path** in `source` (`cliPath()`); `GET /api/status` uses only the base name.
- **Create:** `type` ∈ {task, project, heading}; `when` ∈ {inbox, today, anytime, someday} (`validWhen`), no `upcoming` (use `scheduled`); title ≤ 500, note ≤ 5000; new to-dos get a second `reorder` write to the top.
- **Rejected logins create no session** — the account is checked with `things-cli today` first. With the account in the environment, login and logout return `403`.
- **Credentials** reach the CLI only via child env vars (`THINGS_USERNAME`/`THINGS_PASSWORD`/`THINGS_TOKEN`/`THINGS_CLI_CACHE`), never argv or URLs; the CLI is run with `exec.Command` (no shell). IDs are validated and values starting with `--` are rejected.
- **Cache** path = `THINGS_CLI_CACHE` → `DATA_DIR/things-cli-state-v8.json` → app root. The CLI writes it, the app reads it. When the CLI state format changes, rename the file (`-vN`) everywhere it is named (`grep -rn things-cli-state`).
- **Off loopback in basic mode, the app refuses to start** without an `APP_PASSWORD` of ≥ 16 characters. Passkey mode needs `APP_PASSKEY_ORIGINS` or `APP_PUBLIC_ORIGINS` (https, or `http://localhost`); each origin is its own relying party.

### Key files

| Path | Purpose |
| --- | --- |
| `cmd/nubererum/main.go` | Entry point, CLI adapter, sync model, list rules and order, status/login/logout/sync/complete/create |
| `cmd/nubererum/modelcache.go` | In-memory model cache, background refresh, `?fresh=1` |
| `cmd/nubererum/writes.go`, `edits.go`, `details.go`, `reorder.go` | Write endpoints, `writeMu`, retries, `writePaths`, `runCLIBatch`, `planReorder` |
| `cmd/nubererum/archive.go` | `GET /api/logbook`, `GET /api/trash` |
| `cmd/nubererum/security.go` | Config check, Basic Auth lockout, HTTPS redirect, headers, same-origin guard, middleware chain |
| `cmd/nubererum/passkey.go` | WebAuthn, sessions, setup codes, `passkey-setup`/`passkey-list`/`passkey-remove` |
| `cmd/nubererum/logging.go`, `compress.go` | Logs with account redaction, `/api/client-error`; gzip |
| `cmd/nubererum/*_test.go` | Go tests pinning the invariants |
| `public/index.html`, `public/app.js`, `public/styles.css` | Frontend (no build step) |
| `public/shortcuts.js` | Keyboard shortcut table after Things for Mac and the cheat sheet (`?`); loaded before `app.js` |
| `public/theme.js` | Light/dark/system appearance; loaded in `<head>` of both pages so nothing flashes |
| `public/login.html`, `public/login.js` | Passkey login page |
| `Dockerfile`, `compose.yaml`, `.env.example` | Image (builds the patched `things-cli`) and Compose config |
| `patches/things-cloud-sdk-arthursoares.patch` | SDK patch: old history kinds, list rules, write operations |
| `scripts/build-things-cli.sh` | Build the patched `things-cli` into `bin/` |
| `scripts/start-nubererum.sh` | Local launcher (development) |
| `scripts/compare-things-parity.js`, `scripts/export-things-reference.applescript` | Parity comparator and Mac exporter |
| `tests/app.spec.js`, `tests/passkey.spec.js`, `playwright.config.js` | Playwright tests (API mocked; passkey test needs `PASSKEY_CONTAINER`) |
| `tests/parity.test.js` + `tests/fixtures/parity/*.json` | Parity unit tests |

### Commands

- Build: `go build -o nubererum ./cmd/nubererum`
- Build the CLI: `scripts/build-things-cli.sh`
- All checks: `scripts/check.sh` (gofmt, vet and Go tests in a container, parity unit tests, Playwright; `--race` adds the race run); prints `go: ok`, `parity: ok` and the Playwright count, exits non-zero on any failure
- Test (Go): `go test ./...` · Vet: `go vet ./...` · Without local Go: `docker run --rm -v "$PWD":/src -w /src golang:1.26-alpine sh -c 'gofmt -l . ; go vet ./... && go test -count=1 ./...'`
- Browser tests: `npm install && npx playwright install chromium && npm run test:e2e`
- Passkey browser test: `PASSKEY_CONTAINER=<name> npx playwright test tests/passkey.spec.js`
- Parity: `npm run test:parity` (unit) · `npm run compare:parity -- --reference .things-parity/reference.json` (live)
- Docker: `docker compose up -d --build`

### Security guardrails for agents

- Never commit or echo real credentials (`.env`, `THINGS_*` values, `APP_PASSWORD` values), `things-cli-state*.json`, `passkeys.json`, `passkey-setup.json`, `bin/`, or local binaries.
- Do not run CLI write commands or app write endpoints against the real account unless the owner asks; test instances with a real account run with `APP_READ_ONLY=1`.
- Preserve Basic Auth and its lockout, passkey checks, the same-origin guard, `exec.Command` argv usage, input validation, account redaction in logs, and the static-file containment check when editing the server.
- Keep the Today-membership, visible-filter and logbook invariants intact; update the guarding tests together with any change.
- The MkDocs site maps to `mkdocs.yml` under `docs/`.
