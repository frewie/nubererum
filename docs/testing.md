# Testing

There are three test layers. None of them needs a Things Cloud account; only the live parity comparison and the passkey browser test need a running app.

## All checks at once (`scripts/check.sh`)

```sh
scripts/check.sh          # gofmt, go vet, go test (in golang:1.26-alpine), parity unit tests, Playwright
scripts/check.sh --race   # also go test -race (in golang:1.26 with CGO_ENABLED=1)
```

It prints `go: ok`, `parity: ok` and Playwright's passed count, shows only the failing output otherwise, and exits non-zero on any failure. The Go steps need Docker (the `gomodcache` volume keeps the modules); Playwright needs Node and Go on the host (`go run`, see below).

## 1. Go tests (`go test ./...`)

The tests live next to the code in `cmd/nubererum/*_test.go`. Run them from the repository root:

```sh
go vet ./...
go test ./...
```

Without a local Go, run the checks in a container:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.26-alpine sh -c 'gofmt -l . ; go vet ./... && go test -count=1 ./...'
```

Tests that need `things-cli` use small shell scripts as stand-ins and write a cache file in the format the patched CLI writes, so they need `/bin/sh`. No test contacts Things Cloud.

| File | Covers |
| --- | --- |
| `main_test.go` | List rules (`TestStateViewsMatchScheduleBuckets`, `TestProjectWhenMatchesThingsLists`), note cleaning, the Today invariant (`TestApplyCLIToday*`, `TestCompletedTodayStaysInTodayAfterCLIToday`), the visible-task filter and recounts, Things' order (`TestSortTasksMatchesThingsOrder`), the sync pipeline against a stand-in CLI (`TestSyncModelUsesThingsCLI`), the logbook rule, create validation and payloads (`TestValidWhen`, `TestCreatePassesTypeAndStartDate`), read-only mode, rejected logins, `DATA_DIR`. |
| `projects_test.go` | Project progress, status and details in the model; completing (to-dos and checklists first, then the project) and cancelling the rest; schedule, deadline and tags for projects; renaming and deleting headings; deleting areas (content to the Trash first, then the area); Empty Trash (children before projects, in batches). `edits_test.go` also covers reminders (start date, removal, no date); making a to-do repeat (template fields, original trashed, rule checks); `tags_test.go` renaming and deleting tags (model patch); Convert to Project (the write sequence, and stopping at a failed step); search of the Logbook (title and notes, order, limits). |
| `edits_test.go` | Update, checklist actions, trash and restore, reopen and log completed, schedule, deadline and tags, Logbook and Trash from the cache, account redaction, failed-request logging. |
| `writes_test.go` | Write retries only after errors that prove nothing was written, one write at a time, move keeps the schedule and takes Inbox to-dos to Anytime, the explained refusal for repeating templates. `edits_test.go` also covers `/api/schedule` with `evening` and `/api/cancel`. |
| `reorder_test.go` | `planReorder` (gaps, renumbering, Today day), one batch per reorder, projects and areas in the sidebar, project to area. |
| `cliworker_test.go` | The serve worker: one process for many commands, offline flag and batch input, errors keep it, crashes and timeouts restart it, another account gets its own, fallback without serve mode. |
| `modelcache_test.go` | One sync at a time, stale model with background refresh, `fresh` waits for a sync after a write, reset, copy-on-write patches, gzip. |
| `security_test.go` | Password required off loopback, client IP from the nearest proxy, Basic Auth lockout, same-origin guard and JSON-only writes, `APP_PUBLIC_ORIGINS`, HTTPS redirect, security headers, CLI input checks, login disabled with a server-managed account. |
| `passkey_test.go` | Passkey config, session required, registration with a setup code and login, user verification, bad assertions, rate limiting, removal ends sessions, setup code expiry, session expiry, cross-origin posts, passkeys bound to their origin. |

## 2. Playwright browser tests (`npm run test:e2e`)

`playwright.config.js` starts `go run ./cmd/nubererum` on `127.0.0.1:4180` with `APP_PASSWORD` and all Things credentials cleared. `tests/app.spec.js` mocks the `/api/*` routes in the browser and acts as a stand-in for the server, so it tests the page, not Things Cloud.

```sh
npm install
npx playwright install chromium
npm run test:e2e
```

`tests/app.spec.js` covers, among others: the loading skeleton and the cached model replaced after refresh; completing, failed completion and Log Completed; quick add in every list (Upcoming asks for a start date) and new to-dos at the top; new projects in an area; the sidebar order by area and Someday projects; dragging to-dos, projects, areas and checklist items, and a failed reorder; moving selected to-dos into another project; headings (also renaming and deleting them); deleting an area (its counts in the question, Undo without the area); projects (progress pie, Complete Project with its question, header When, deadline and tags); cancelling (`Ctrl+Alt+K`, Alt+click, the editor button); This Evening (its section in Today, `Ctrl+E`, the When menu, dragging in and out); editing title, notes, checklist, when, deadline and tags; Delete with restore; keyboard shortcuts (lists, search, arrows, Enter, Ctrl+K; Alt+T, Ctrl+R and Ctrl+O; start date and deadline shifts; the Move dialog and navigation popover; select all, Space below the selection, paste; the cheat sheet); Logbook reopen and Trash restore and Empty Trash; reminders in the editor (only with a date) and on rows; the Repeat dialog with Undo and `Ctrl+Shift+R`; the tag bar and the Tags dialog; area tags in the area header and inherited in the tag bar; Convert to Project with Undo; Logbook matches in search, the Logbook by month and Show more; Markdown in notes (rendering, escaping, `Ctrl+B`/`Ctrl+I`); folding areas (reload, keys, count, drops); browser error reports; long press on a touch screen; the Settings disconnect.

### Passkey browser test

`tests/passkey.spec.js` runs the passkey flow (register with a setup code, sign out, sign in, remove) with a virtual authenticator against a running container in passkey mode. It is skipped unless `PASSKEY_CONTAINER` is set; `PASSKEY_BASE_URL` defaults to `http://localhost:3007`. It calls `docker exec <container> nubererum passkey-setup`. See [Deployment — Passkey Sign-In](DEPLOYMENT.md#passkey-sign-in) for the throwaway container.

```sh
PASSKEY_CONTAINER=things-passkey-test npx playwright test tests/passkey.spec.js
```

## 3. Parity (`npm run test:parity`, `npm run compare:parity`)

The parity tools compare the app with Things for Mac by task UUID. They never print titles or notes.

### Comparator unit tests

```sh
npm run test:parity
```

`tests/screenshot.spec.js` writes `docs/screenshot.png` from demo data (`npm run screenshot`); it is skipped in the normal run. GitHub Actions (`.github/workflows/ci.yml`) runs gofmt, vet, the Go tests with the race detector, the parity and browser tests, and `docker build .` on every push and pull request; Dependabot (`.github/dependabot.yml`) checks Go modules, npm, actions and the Docker base images weekly.

`tests/parity.test.js` runs `compareManifests` and `appRequestHeaders` from `scripts/compare-things-parity.js` against `tests/fixtures/parity/*.json`. It checks that matching membership and order pass; that status, project, area, order, leaks and duplicate UUIDs are reported; that projects in Things lists are ignored and to-dos completed today are matched; that Basic Auth goes into a header, not the URL; and that the CLI exits non-zero with a privacy-safe summary.

### Live comparison

Follow [Parity Testing](PARITY_TESTING.md): export a reference on the Mac with `scripts/export-things-reference.applescript`, copy it to `.things-parity/`, then run

```sh
npm run compare:parity -- --reference .things-parity/reference.json
```

against the running app (Basic Auth through `PARITY_APP_USERNAME` and `PARITY_APP_PASSWORD`).

## Not covered

- **Writes against the real Things Cloud account.** Creates, completes, edits, moves and reorders are tested with stand-ins and by the SDK patch's write checks. The CLI has no dry run. Use `APP_READ_ONLY=1` for test instances with a real account.
- **The CLI itself.** `things-cli` comes from `arthursoares/things-cloud-sdk`; the patch is tested in the SDK checkout (see [Deployment — Patched Things CLI](DEPLOYMENT.md#patched-things-cli)).
- **The container build and runtime** are not part of the automated tests.
