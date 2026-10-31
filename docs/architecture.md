# Architecture

NubeRerum is one Go binary (module `nubererum`, `go 1.26.4`) plus a static frontend in `public/` without a build step. It has no database. It runs `things-cli` as a child process and reads the CLI's state cache. Its only Go dependency is `github.com/go-webauthn/webauthn` for passkeys.

## Components

| Component | File | Responsibility |
| --- | --- | --- |
| Entry point | `cmd/nubererum/main.go` (`main`) | Logging, passkey subcommands, `HOST`/`PORT`, config check, routes, warm-up sync, HTTP server. |
| API routes | `main.go` (`apiHandler`) | Dispatches `/api/*`; status, login, logout, sync, complete and create are handled inline. |
| CLI adapter | `main.go` (`cliPath`, `runCLI*`, `cliEnv`, `readWithRetry`), `reorder.go` (`runCLIBatch`) | Finds and runs `things-cli`. |
| Model builder | `main.go` (`syncModel`, `syncModelFromCache`, `stateViews`, `applyVisibleTaskFilter`, `applyCLIToday`, `sortTasks`, `logbookRule`) | Builds lists, projects, areas, headings and tags from the CLI. |
| Model cache | `modelcache.go` | Keeps the last model in memory, refreshes it in the background, serialises syncs. |
| Writes | `writes.go` (`writeMu`, `withWriteRetry`, move), `edits.go` (update, checklist, trash, reopen, log completed, create helpers), `details.go` (schedule, deadline, tags, tag and area create, project area), `reorder.go` (reorder) | Change Things Cloud through `things-cli`. |
| Logbook and Trash | `archive.go` | Read completed and trashed items from the cache file. |
| Security | `security.go` | Config check, Basic Auth with lockout, HTTPS redirect, headers, same-origin guard, input checks, server timeouts. |
| Passkeys | `passkey.go` | WebAuthn sign-in, sessions, setup codes, `passkey-*` subcommands. |
| Compression | `compress.go` | gzip for JSON, HTML, CSS, JS, SVG and the manifest. |
| Logging | `logging.go` | key=value logs on stderr, failed-request logging, account redaction, `/api/client-error`. |
| Static files | `main.go` (`staticHandler`) | Serves `public/` with a containment check. |
| Frontend | `public/index.html`, `public/app.js`, `public/shortcuts.js`, `public/styles.css`, `public/login.html`, `public/login.js`, `public/manifest.webmanifest`, icons | UI, keyboard shortcuts and passkey login page. |

## Request path

```
Browser
  │
  ▼
securityHeaders → httpsOnly → basicAuth → gzipResponses → logErrors → sameOriginGuard → mux        (APP_AUTH=basic)
securityHeaders → httpsOnly → gzipResponses → logErrors → sameOriginGuard → passkeys.middleware → mux  (APP_AUTH=passkey)
                                                                                                   │
                                     ┌─────────────────────────────────────────────────────────────┤
                                     ▼                                                             ▼
                          staticHandler (public/)                                         apiHandler (/api/*)
                                                                                                   │
                                                                     models (cache) ◀──────────────┤
                                                                           │                       │ writes (writeMu)
                                                                           ▼                       ▼
                                                                 things-cli list / today     things-cli batch / edit / complete / create-*
                                                                           │                       │
                                                                           └────── Things Cloud ───┘
                                                                           │
                                                                           ▼
                                                                 things-cli-state-v8.json (written by the CLI, read by the app)
```

`appHandler` in `security.go` builds the chain. Basic Auth is a no-op when `APP_PASSWORD` is empty (allowed only on loopback).

## CLI adapter

- `cliPath()` finds the binary (see [Setup — things-cli discovery](setup.md#things-cli-discovery)).
- Commands run with `exec.Command(cli, args...)`: an argument vector, no shell. Batch operations go to `things-cli batch` as JSON on stdin.
- The account reaches the CLI only through the child environment: `THINGS_USERNAME`, `THINGS_PASSWORD`, `THINGS_TOKEN` and `THINGS_CLI_CACHE`. Never through arguments or URLs.
- A non-zero exit becomes `502` with the trimmed stderr; write errors are redacted (the account is replaced by `<account>`). Output that is not JSON also becomes `502`.

## Reading: sync pipeline

`syncModel()`:

1. `things-cli list`: the open to-dos. The CLI skips completed, canceled and trashed ones.
2. `things-cli today`: Today membership, in CLI order.
3. `syncModelFromCache()` reads the cache file and builds the model:
   - Areas, tags and open projects (`Type 1`) and headings (`Type 2`) of open projects.
   - Open to-dos (`Type 0`, `Status 0`, not in the Trash, no completion date), plus completed and canceled to-dos that Things still lists because they are not logged yet (`logbookRule`, from the cached Things settings `LogDate` (`ld`) and `LogInterval` (`li`): daily keeps today's completions, manual keeps all since the last "Log Completed", `0` keeps none).
   - Each to-do gets its lists from `stateViews`: Inbox (`Schedule 0`), Upcoming (future start date or repeating template), Someday (`Schedule 2` without date and outside a project), Anytime (`Schedule 1`, started, in a started project). Today is added later.
   - Projects get `when` (`anytime`, `someday`, `upcoming`) from `projectWhen`.
   - Checklists come from `CheckListItems`.
4. `applyVisibleTaskFilter`: keeps only to-dos that `list` returned, plus the completed ones from step 3, and recounts projects and areas.
5. `applyCLIToday`: Today becomes exactly the `today` rows, plus completed to-dos that were in Today by date and are not logged yet.
6. `sortTasks` orders every list as Things does: loose to-dos first, then by area and project position; Today by the day a to-do entered Today (newest first) and its Today position; other lists by heading and position; Upcoming by start date.

Read commands are retried once after a failed name lookup or refused connection.

### Today invariant

Today membership comes only from `things-cli today`. Cache fields such as `TodayIndex` and `TodayIndexReferenceDate` only order the rows. The one exception is a completed to-do that is not logged yet: it keeps its place, as in Things (the CLI lists open to-dos only). Tests: `TestApplyCLITodayReplacesCacheTodayAndKeepsCLIOrder`, `TestApplyCLITodayEmptyResultClearsCacheToday`, `TestApplyCLITodayKeepsManuallyMarkedTaskWithOldDate`, `TestCompletedTodayStaysInTodayAfterCLIToday`.

### Model cache

`modelcache.go` keeps the last model:

- `GET /api/sync` answers from the cache at once. When the model is older than 30 seconds, a background sync starts.
- `GET /api/sync?fresh=1` waits for a sync that started after the last write.
- Only one sync runs at a time, because parallel CLI runs would race on the cache file.
- After a write, `models.wrote(edit)` patches a copy of the cached model (stored models are never modified) and starts a sync; if one is already running, another one follows it.
- Login and logout reset the cache; a sync still running for the old account is discarded.
- At start, when the account comes from the environment and the CLI exists, `main()` starts a warm-up sync.

### Logbook and Trash

`GET /api/logbook` and `GET /api/trash` read the cache file directly (`archive.go`). They return at most 300 items by default (`?limit=` up to 2000) and the total count.

## Writes

- All writes go through `things-cli`: `batch` for most operations, `edit` for title and notes, `complete`, `create-tag` and `create-area`.
- `cliworker.go` keeps one `things-cli serve` process running (serve mode is part of the SDK patch). It signs in once, keeps the connection to Things Cloud open and the state in memory, and runs commands one after another, so reads and writes never race on the cache file. A sync is `list` plus an offline `today` on the same state. A one-process-per-command run took about 1.7 s, almost all of it network round trips for signing in. A command that does not answer within two minutes ends the worker (`504`, not retried); a crash is reported with the CLI's stderr; the next command starts a new worker. `THINGS_CLI_WORKER=0`, or a `things-cli` without serve mode, runs one process per command as before.
- Before every change to an existing to-do, the SDK checks in the history that the target exists and is unambiguous (`preflightTask7Modifications`). Headings are renamed with `/api/update` and deleted with `/api/heading-delete` (their to-dos stay in the project). Deleting an area (`/api/area-delete`) trashes its projects and to-dos first, as current Things does, then deletes the area; Undo restores them without it. Empty Trash (`/api/empty-trash`, `archive.go`) deletes every Trash item for good as Things does (Task7 action 2 plus a `Tombstone2` in one commit, 50 items per write); next to deletions the SDK accepts only tombstones for items deleted in the same commit. Reminders (`/api/reminder`) write `ato` with the start date; the SDK accepts `ato` null or 0..86399 in modifications. Repeat… (`/api/repeat`, `repeat.go`) creates a template with a validated `rr` (`validateRepeatRule`, SDK patch: only the shapes Things wrote in the owner's history) and trashes the original in the same write; the Things apps create the to-dos from `icsd` on. Tags are renamed and deleted from the Tags dialog (`tags.go`: Tag4 `{tt}` and action 2); a tag bar above each list filters it by one tag (to-dos and, in Someday and Upcoming, projects; the filter belongs to its list); items match the tags of their project and area too, as in Things. Areas carry tags (`/api/tags` writes `area-tags`, Area3 `{tg}`, as Things does), set in the area header. Convert to Project (`/api/convert-project`) has no single Things write to copy, so it chains verified ones: create the project, create its to-dos, then order, complete and trash the original. Quick Find also asks `GET /api/search` for Logbook matches (read from the cache file); the Logbook is grouped by month. Areas fold in the sidebar (triangle, Left/Right on a focused area; remembered per browser in `localStorage` `things-collapsed-areas`); a folded area shows its open to-dos, still takes dropped to-dos and opens after a dragged to-do rests on it for 600 ms. Notes with Markdown show a rendered preview below the notes field (`markdownHtml`: text is escaped before tags are added; links go through `renderSafeLink`); `Mod+B`/`Mod+I` wrap the selection. Projects carry `status`, `done`/`total` (progress pie) and their When, deadline and tags; completed and canceled projects stay until logged, like to-dos (`projects.go`). The model gives each to-do `evening` (`sb`; Today shows these under "This Evening" at the end, and dragging into or out of that section schedules `evening` or `today`) and `repeat`, a summary of its repeat rule (`repeat.go`; for an instance the template's rule). Instances of repeating to-dos (`rt` names the template) take every change, as in Things; the template itself (`rr` set) takes only a new title, notes and the Trash, and the app answers other changes with `409` and an explanation (`explainWriteError`). A full check reads the whole history (about 253,000 items, ~30 s). The worker keeps the result for every item and the index it has read up to (`KeepPreflightIndex`, SDK patch), so later checks read only new items (~0.2 s). It reads the history once in the background when it starts; a write that comes earlier waits for that. One-shot `things-cli` runs still read the whole history.
- New to-dos are answered after the create; the move to the top of the list (a second write) runs afterwards. The page sends `placed: true` when it places the to-do itself (below a selected one).
- `writeMu` runs writes one at a time. Things Cloud accepts a write only on top of the latest history state and answers parallel writes with `409 OutdatedAncestor`.
- `withWriteRetry` retries up to three times (after 1, 2 and 4 seconds) only when nothing can have been written: `OutdatedAncestor`, failed name lookups, refused connections. Timeouts are not retried.
- Most handlers check the cached model first and answer `409` when a referenced item is missing.
- `APP_READ_ONLY=1` rejects every path in `writePaths` (`edits.go`) with `403`.
- The batch operations beyond v0.6.0 (`reorder`, `area-reorder`, `reopen`, `restore`, `move`, `unfile`, `schedule`, `deadline`, `tags`, `log-completed`, `checklist-*`, `create` positions) come from `patches/things-cloud-sdk-arthursoares.patch`. Each writes the fields Things itself writes for that change (see [Deployment — Patched Things CLI](DEPLOYMENT.md#patched-things-cli)).
- Reordering (`planReorder`) puts the moved item between its neighbours with gaps of 512 and renumbers a section only when there is no room. In Today it writes `todayIndex` and, when the day changes, `todayDate`.

## Session model

- **Things account.** One account per process, in memory (`session`). It comes from the environment (`THINGS_USERNAME`/`THINGS_PASSWORD` and fallbacks) or from `POST /api/login`, which first checks it with `things-cli today`. When it comes from the environment, login and logout are disabled.
- **Basic Auth** (`APP_AUTH=basic`). Constant-time comparison. 5 failures in 15 minutes block a client for 15 minutes; after 50 failures from all clients in 15 minutes, one failure blocks. The client address is the rightmost `X-Forwarded-For` entry.
- **Passkeys** (`APP_AUTH=passkey`). One relying party per origin in `APP_PASSKEY_ORIGINS` or `APP_PUBLIC_ORIGINS`. Discoverable credentials with user verification. A session is a random token in the cookie `__Host-things_session` (`Secure`, `HttpOnly`, `SameSite=Strict`), kept hashed in memory for 30 days; a restart signs everyone out. Passkeys are stored in `DATA_DIR/passkeys.json` (0600). The first passkey on an origin needs a one-time setup code from `nubererum passkey-setup` (stored hashed in `passkey-setup.json`, valid 30 minutes). Failures share the Basic Auth lockout.

## Middleware

| Middleware | Behaviour |
| --- | --- |
| `securityHeaders` | CSP `default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'`, plus `nosniff`, `no-referrer`, `X-Frame-Options: DENY`, `Permissions-Policy`, COOP, CORP, `X-Robots-Tag`. HSTS when the proxy reports HTTPS. `no-store` for `/api/`, `private, no-cache` otherwise. |
| `httpsOnly` | Redirects requests with `X-Forwarded-Proto: http` to HTTPS (308). |
| `basicAuth` | See above; only in basic mode and only with `APP_PASSWORD`. |
| `gzipResponses` | Compresses successful text and JSON responses. |
| `logErrors` | Logs failed `/api/` and `/auth/` requests (5xx as ERROR, others as WARN; a 401 is not logged) with the account redacted. |
| `sameOriginGuard` | `GET`/`HEAD` pass. Other methods must be `POST`, same origin (or `APP_PUBLIC_ORIGINS`), no cross-site `Sec-Fetch-Site`, `Content-Type: application/json`; body limit 64 KiB. |
| `passkeys.middleware` | Serves `/auth/*` and `/login`; other requests need a session (`401` for `/api/*`, redirect to `/login` for pages). |

The HTTP server sets header (10 s), read (30 s), write (10 min, for the first sync of a large history) and idle (2 min) timeouts.

## Frontend

`public/app.js` and `public/shortcuts.js` are plain scripts without dependencies:

- On load it calls `GET /api/status`, then `GET /api/sync`. When that answer is refreshing or older than 30 seconds, it shows it and then replaces it with `GET /api/sync?fresh=1`. It refreshes when the page becomes visible again after a minute and every five minutes while visible.
- Lists: Inbox, Today, Upcoming, Anytime, Someday, Logbook and Trash. The sidebar shows projects without an area, then each area with its active projects, in Things' order; Someday and Upcoming projects appear at the top of those lists. Projects are grouped by heading, Upcoming by day.
- Changes show at once and go back if the request fails. New to-dos appear at the top. Completed to-dos stay greyed out until logged.
- The editor edits title, notes, checklist, when, deadline and tags in place.
- Selection with click, Shift and Ctrl/Cmd.
- Keyboard shortcuts live in `public/shortcuts.js` (loaded before `app.js`). They follow Things for Mac: Mod is Cmd on a Mac and Ctrl elsewhere; keys are matched by `event.code`. Groups: create (`n`, Space below the selection, project, heading, paste one to-do per line), edit (Enter, Esc, Mod+K complete, Mod+D duplicate, Mod+C copy titles, Mod+Shift+Y log completed, Delete/Backspace trash with Undo), select (arrows, Shift, Alt, Mod+A), move (Mod+arrows, Mod+Shift+M Move dialog), when and deadline (Mod+T/R/O for Today/Anytime/Someday, Ctrl+]/[ and Ctrl+./, to shift start date and deadline), tags, navigate (`1`–`6` and Mod+1–6, Mod+Shift+O navigation popover, Mod+L show in parent, Mod+← back, Mod+/ sidebar, `/` and Mod+F search, `r` refresh). Combinations browsers keep (Mod+N, T, W and Shift variants) also work with Alt. `?` or "Shortcuts" in the sidebar opens a cheat sheet generated from the same table; Things shortcuts the app cannot offer (This Evening, cancel, repetition, Markdown) are greyed out.
- Drag and drop with pointer events: reorder to-dos, checklist items, projects and areas; drop selected to-dos on a project, area, heading, Today, Anytime or Someday; move projects between areas. On touch screens a long press selects or starts a drag.
- Notes are rendered as text; `http(s)://` and `www.` links become anchors with `rel="noopener noreferrer"`; other schemes are refused.
- Browser errors and failed requests are reported to `/api/client-error` (at most 20 per page).
- The last opened list is remembered in `localStorage` (`things-view`). Credentials are never stored in the browser.
- In passkey mode, a 401 sends the page to `/login`; the sidebar shows a "Passkeys" link. `public/login.js` runs the WebAuthn ceremonies.

## Trust boundary

```
┌──────────────────────┐  HTTPS via proxy, Basic Auth or passkey  ┌──────────────────────────────┐
│  Browser (owner)     │ ───────────────────────────────────────▶ │  Go server (this repo)       │
└──────────────────────┘                                          │  ─ Things account in memory  │
                                                                  │  ─ runs things-cli           │
                                                                  └──────────────┬───────────────┘
                                                                                 │ exec (argv) + env
                                                                                 ▼
                                                                  ┌──────────────────────────────┐
                                                                  │  things-cli (v0.6.0+patch)   │
                                                                  │  ─ writes the state cache    │
                                                                  └──────────────┬───────────────┘
                                                                                 │ HTTPS
                                                                                 ▼
                                                                  ┌──────────────────────────────┐
                                                                  │  Things Cloud                │
                                                                  └──────────────────────────────┘
```

## Design notes

- **One account, no per-user model.** Whoever passes the app's authentication operates the account.
- **`GET /api/sync` shows the full CLI path** in `source`; `GET /api/status` shows only the base name.
- **`backend.directCloud` is always `true` and `backend.nativeFallback` always `false`.** These are constants. Without `things-cli`, `backend.mode` is `missing-cli` and every route that needs the CLI returns `424`; the page then says "things-cli is missing on the server".
