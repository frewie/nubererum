# API Reference

One Go process serves the JSON API under `/api/`, the passkey endpoints under `/auth/` (passkey mode only), the MCP endpoint `/mcp` for coding agents and the static frontend. Handlers live in `cmd/nubererum/`: `main.go` (`apiHandler`, status, login, logout, sync, complete, create), `edits.go`, `details.go`, `writes.go`, `reorder.go`, `archive.go`, `logging.go`, `passkey.go` and `mcp.go`.

## Common rules

- **Authentication.** In `APP_AUTH=basic` (default), Basic Auth protects every route when `APP_PASSWORD` is set. In `APP_AUTH=passkey`, every route except `/login`, `/auth/*` and the login page assets needs a session cookie; unauthenticated `/api/*` calls get `401` `{"error":"Sign in with your passkey.","login":"/login"}`. `/mcp` is the exception in both modes: it accepts only its own Bearer tokens (see [MCP endpoint](#mcp-endpoint-mcp)).
- **State-changing requests** must be `POST` with `Content-Type: application/json` and an `Origin` (or `Referer`) whose host matches the request host or an entry of `APP_PUBLIC_ORIGINS`. A cross-site `Sec-Fetch-Site` is rejected. Other methods than `GET`, `HEAD` and `POST` get `405`. Bodies are limited to 64 KiB.
- **Read-only mode.** With `APP_READ_ONLY=1`, every write endpoint (marked *write* below) returns `403` `{"error":"This instance is read-only (APP_READ_ONLY)."}` without running `things-cli`.
- **Identifiers** (to-do, project, heading, area, tag, checklist item) must match `^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`. Titles, notes and names that start with `--` are rejected, so they cannot be read as CLI options.
- **Writes** run through `things-cli`, one at a time, with retries only for errors that prove nothing was written (see [Architecture — Writes](architecture.md#writes)). After a write the server patches its cached model and starts a new sync.
- **Conflicts.** Many writes check the server's cached model first. When a referenced to-do, project, area, heading, tag or checklist item is not there, they return `409` with "… changed. Refresh and try again."
- JSON responses carry `Cache-Control: no-store` and are gzip-compressed when the client accepts it.

Errors use one envelope:

```json
{ "error": "<message>" }
```

## Session and status

### `GET /api/status`

```json
{
  "signedIn": true,
  "email": "<Things Cloud email>",
  "serverManaged": true,
  "auth": "basic",
  "backend": { "mode": "things-cli", "cli": "things-cli", "nativeFallback": false, "directCloud": true }
}
```

| Field | Meaning |
| --- | --- |
| `signedIn` | An email and a password are set (environment or browser sign-in). |
| `serverManaged` | The account comes from the environment; login and logout are disabled. |
| `auth` | `basic` or `passkey`. |
| `backend.mode` | `things-cli` when a CLI binary is found, else `missing-cli`. |
| `backend.cli` | Base file name of the CLI, or `.` when none was found (`filepath.Base("")`). |
| `backend.nativeFallback`, `backend.directCloud` | Constants (`false`, `true`). There is no reader without the CLI. |

### `POST /api/login`

Body `{"email","password"}`. The server runs `things-cli today` with these credentials and stores them in memory only if that succeeds.

| Status | Meaning |
| --- | --- |
| `200` | `{"signedIn":true,"email":…,"backend":{…}}`. The cached model is dropped. |
| `400` | Email or password empty. |
| `403` | The account is server managed. |
| `424` | CLI not found. |
| `502` | The CLI failed (for example wrong credentials). No session is created. |

### `POST /api/logout`

Clears the in-memory account and the cached model. `200` `{"signedIn":false}`; `403` when the account is server managed.

## Reading

### `GET /api/sync[?fresh=1]`

Returns the task model. Without `fresh`, the answer comes from the server's cached model at once; if it is older than 30 seconds, a background sync starts and `refreshing` is `true`. The first call after a start or account change waits for a sync. With `?fresh=1`, the server waits for a sync that started after the last write through the app.

```json
{
  "generatedAt": "2026-10-27T12:00:00+01:00",
  "syncedAt": "2026-10-27T12:00:00+01:00",
  "refreshing": false,
  "syncError": "only present when the last background sync failed",
  "source": "<full path of the CLI binary>",
  "stats": { "tasks": 120, "projects": 14, "areas": 6, "rawEvents": 4821 },
  "tasks": [],
  "projects": [],
  "areas": [],
  "headings": [],
  "tags": [],
  "lists": { "inbox": [], "today": [], "upcoming": [], "anytime": [], "someday": [] }
}
```

`lists` holds the to-dos of each list in Things' order. `source` is the full CLI path, while `/api/status` shows only the base name.

**Task**

| Field | Meaning |
| --- | --- |
| `uuid`, `title`, `note` | `note` has Things' `<note>` markup removed and entities unescaped. |
| `status` | `open`, `completed` or `canceled`. Completed and canceled to-dos appear until they are logged. |
| `type` | `task`. |
| `schedule` | First entry of `views`. |
| `views` | Lists the to-do appears in: `inbox`, `today`, `upcoming`, `anytime`, `someday`. |
| `scheduledDate`, `deadline`, `modificationDate` | `YYYY-MM-DD` or `""`. |
| `project`, `projectTitle`, `heading`, `headingTitle`, `area`, `areaTitle` | Where the to-do belongs; `""` when not set. |
| `tags`, `tagIDs` | Tag titles and tag UUIDs. |
| `checklist` | `[{ "uuid", "title", "done", "index" }]` in Things' order. |
| `index`, `todayIndex`, `todayDate`, `areaIndex`, `projectIndex`, `headingIndex` | Positions used for ordering and reordering. |
| `evening` | `true` when the to-do is in This Evening (Things' `sb` 1). |
| `repeat` | How the to-do repeats, e.g. `Every week on Friday`, `Every month on the last day`, `Every 2 days after completion`; the template's rule for an instance; `""` when it does not repeat. |

**Project**: `uuid`, `title`, `type: "project"`, `count` (to-dos in the model), `index`, `area`, `when` (`anytime`, `someday` or `upcoming`), `scheduledDate`, `deadline`, `tags`, `tagIDs`, `status` (`open`, `completed`, `canceled`; finished projects stay until "Log Completed", like to-dos), `done` and `total` (completed and all to-dos for the progress pie, without trashed and canceled ones, logged ones included). **Area**: `uuid`, `title`, `type: "area"`, `count`, `index`. **Heading**: `uuid`, `title`, `project`, `index` (headings of open projects, also empty ones). **Tag**: `uuid`, `title`, `index`.

### `GET /api/logbook` and `GET /api/trash`

Read directly from the CLI cache file, not from the model.

- Logbook: completed and canceled to-dos and projects that are not in the Trash and are already logged, newest `completedAt` first.
- Trash: to-dos and projects in the Trash, newest `modifiedAt` first.
- `?limit=N` (1–2000); default 300.
- The page groups the Logbook by month, newest first, and loads 300 more with "Show more".

`GET /api/search?q=` (2–200 characters) returns the Logbook entries (same shape, `total` and at most 100 `items`, newest first) whose title or notes contain `q`, ignoring case; nothing in the Trash, and nothing still in the lists (the page searches those in its model). Quick Find shows them in a "Logbook" section below the open matches, 250 ms after the last key.

```json
{
  "total": 1234,
  "items": [
    { "uuid": "…", "title": "…", "type": "task", "status": "completed",
      "completedAt": "…", "modifiedAt": "…", "project": "…", "projectTitle": "…", "areaTitle": "…" }
  ]
}
```

## Creating

### `POST /api/create` (write)

```json
{
  "type": "task",
  "title": "Water plants",
  "note": "Kitchen windowsill",
  "when": "inbox",
  "scheduled": "2026-11-02",
  "project": "<project uuid>",
  "heading": "<heading uuid>",
  "area": "<area uuid>"
}
```

| Field | Rule |
| --- | --- |
| `type` | `task` (default), `project` or `heading`. |
| `title` | Required, at most 500 bytes after trimming. |
| `note` | Optional, at most 5000 bytes after trimming. |
| `when` | `inbox`, `today`, `anytime` or `someday`. `upcoming` is not accepted; use `scheduled`. Default: `inbox` for to-dos, `anytime` for projects and headings, none when `scheduled` is set. |
| `scheduled` | Optional start date `YYYY-MM-DD`. A future date puts the to-do in Upcoming. |
| `project`, `heading`, `area` | Optional valid IDs. |
| projects | May only have an `area`; `when: inbox`, `project` and `heading` are rejected. |
| headings | Need a `project` and no `heading` or `area`. |

The server sends one `create` operation to `things-cli batch`. A new to-do then gets a second `reorder` write that moves it to the top of its list, project or area (and of Today when `when` is `today`). A new heading is moved after the project's last heading. A failed second write is logged; the create still succeeds.

Response `200`: `{"created":true,"when":…,"type":…,"uuid":…,"task":{…}}`; `task` is the model entry for a new to-do, else `null`.

### `POST /api/tag-create` and `POST /api/area-create` (write)

Body `{"title"}`, at most 200 bytes. Runs `things-cli create-tag <title>` or `things-cli create-area <title>`. Response `{"ok":true,"uuid":…,"title":…}`.

## Changing to-dos

| Endpoint | Body | Effect | Response |
| --- | --- | --- | --- |
| `POST /api/complete` (write) | `{"uuid"}` | `things-cli complete <uuid>`. The to-do stays in its list as completed. | `{"completed":true,"uuid":…}` |
| `POST /api/reopen` (write) | `{"uuid"}` | Batch `reopen`. | `{"ok":true}` |
| `POST /api/cancel` (write) | `{"uuids":[…]}` (1–100) | Batch `cancel` per to-do (`ss` 2 with the time, as Things writes it). The to-dos stay in their lists as `canceled` until "Log Completed"; `/api/reopen` undoes it. Unknown to-dos return `409`. | `{"ok":true}` |
| `POST /api/log-completed` (write) | `{}` | Batch `log-completed`: completed to-dos move to the Logbook, as Things' "Log Completed". | `{"ok":true}` |
| `POST /api/area-rename` (write) | `{"uuid","title"}` | Batch `area-rename` (Area3 `tt`); name non-empty, at most 500 bytes. | `{"ok":true}` |
| `POST /api/update` (write) | `{"uuid","title"?,"note"?}` | `things-cli edit <uuid> --title … --note …` (to-dos and projects; a project's new title also shows on its to-dos). Title non-empty, at most 500 bytes; note at most 5000 bytes, trailing whitespace trimmed. At least one field. | `{"updated":true}` |
| `POST /api/trash` (write) | `{"uuids":[…]}` (1–100) | Batch `trash` per to-do or project; a project takes its to-dos (open and completed) along, as in Things. | `{"ok":true,"count":N}` |
| `POST /api/restore` (write) | `{"uuids":[…]}` (1–100) | Batch `restore` (out of the Trash); a project brings back its trashed to-dos. | `{"ok":true,"count":N}` |
| `POST /api/schedule` (write) | `{"uuids":[…],"when"}` or `{"uuids":[…],"startDate":"YYYY-MM-DD"}` | Exactly one of `when` (`today`, `evening`, `anytime`, `someday`, `inbox`) and `startDate`. Batch `schedule` per to-do; `evening` is Today with Things' `sb` 1 (This Evening), every other schedule writes `sb` 0. A single project uuid is scheduled as a project: `today`, `anytime`, `someday` or a start date; `inbox` and `evening` return `400`. | `{"ok":true}` |
| `POST /api/deadline` (write) | `{"uuid","deadline"}` | `deadline` is `YYYY-MM-DD`, or `""` to remove it. `uuid` is a to-do or a project. | `{"ok":true}` |
| `POST /api/tags` (write) | `{"uuid","tags":[tag uuids]}` | Replaces the tags of a to-do, project or area (at most 50); areas use batch `area-tags` (Area3 `{tg}`), as Things writes them. Unknown tags return `409`. | `{"ok":true}` |
| `POST /api/heading-delete` (write) | `{"uuid"}` | Like Things: every to-do under the heading (also completed ones) moves to the project root (`move` with the project), then the heading is deleted (`delete`, Task7 action 2 `{}`), in one batch. Rename a heading with `POST /api/update`. Unknown headings return `409`. | `{"ok":true,"todos":N}` |
| `POST /api/area-delete` (write) | `{"uuid"}` | Like current Things: the area's projects and to-dos (also completed ones, not those already in the Trash) go to the Trash, then the to-dos of those projects, then the area is deleted (`area-delete`, Area3 action 2 `{}`), in one batch. Restoring `trashed` brings the items back without the area. Unknown areas return `409`. | `{"ok":true,"trashed":[uuids],"count":N}` |
| `POST /api/empty-trash` (write) | — | Like Things: everything in the Trash, plus the to-dos and headings of trashed projects, is deleted for good: per item `delete` (Task7 action 2 `{}`) and `purge` (a `Tombstone2` naming it) in the same commit, to-dos first, then headings, then projects, in writes of 50 items one after the other. Checklist items stay in the history, as Things leaves them. Cannot be undone. | `{"ok":true,"deleted":N}` |
| `POST /api/reminder` (write) | `{"uuid","time"}` (`"HH:MM"` or `""`) | Batch `reminder`: `ato` (seconds after midnight) with the start date it rings on, as Things writes it (`{md, ato, st, sr, tir}`); Today and This Evening use today, Upcoming its start date; other to-dos return `400`. An empty time writes `ato:null`. The model gives every to-do `reminder` (`"HH:MM"` or `""`). | `{"ok":true}` |
| `POST /api/repeat` (write) | `{"uuid","type","unit","every","month"?,"start"}` (type `fixed` or `after`; unit `day`, `week`, `month` or `year`; every 1–99; month `day`, `last` or `weekday`) | Makes an open, not yet repeating to-do repeat: one batch creates a template (`create` with `repeat`: `rr` version 4 with `ia` = start, `sr` = today, no end, `icsd` = start, `st` 2, no start date) with the title, notes, project or area, heading and tags, and trashes the original. The Things apps create the to-dos. `start` must not be in the past; for `month:"last"` it must be a month's last day, for `"weekday"` in its first four weeks. | `{"ok":true,"uuid":"<template>"}` |
| `POST /api/tag-rename` (write) | `{"uuid","title"}` | Batch `tag-rename` (Tag4 `{tt}`); name non-empty, at most 200 bytes. Patches the tag list and the tag names on to-dos, projects and areas. | `{"ok":true}` |
| `POST /api/tag-delete` (write) | `{"uuid"}` | Batch `tag-delete` (Tag4 action 2 `{}`), as Things deletes a tag; to-dos keep the id, and the model drops unknown ids. | `{"ok":true}` |
| `POST /api/convert-project` (write) | `{"uuid"}` | Like Things' Convert to Project, from verified writes in three batches: a project with the to-do's title, notes, area, When (Inbox and This Evening become Anytime and Today), deadline and tags; one to-do per checklist item in the project; then their order, the completed ones and the original to the Trash. A failed step stops with `502` and says whether the project exists; the original stays until the last step. Repeating to-dos return `409`. | `{"ok":true,"uuid":"<project>","todos":N}` |
| `POST /api/project-complete` (write) | `{"uuid","mode","rest"?}` | `mode` and `rest` are `complete` or `cancel`; `rest` (default: `mode`) is what happens to the open to-dos, as Things asks. One batch: for each open to-do (headings included) its open checklist items when `rest` is `complete`, then the to-do, then the project. A project that is not open returns `409`. | `{"ok":true,"todos":N}` |

### `POST /api/checklist` (write)

Body `{"action","task","uuid","title","done","order"}`; `task` is the to-do.

| `action` | Needs | Batch operation |
| --- | --- | --- |
| `add` | `title` (at most 500 bytes) | `checklist-add`, placed after the last item. |
| `edit` | `uuid`, `title` | `checklist-edit` |
| `complete` | `uuid`, `done` | `checklist-complete` (ticks or unticks). |
| `delete` | `uuid` | `checklist-delete` |
| `reorder` | `uuid` (the moved item), `order` (all item UUIDs in the new order) | `checklist-reorder` for the moved item, or for the items that need new positions. |

Response `{"ok":true,"uuid":…}`; for `add`, `uuid` is the new item.

### `POST /api/move` (write)

Body `{"uuids":[…] (1–100)}` plus exactly one of `project`, `area`, `heading` or `when` (`today`, `anytime`, `someday`).

| Target | Batch operation |
| --- | --- |
| `when` | `edit` with `when` |
| `project` | `move` (schedule unchanged); from the Inbox `move-to-project` (to Anytime) |
| `area` | `move`; from the Inbox `move-to-area` (to Anytime) |
| `heading` | `move` with `heading`; from the Inbox `edit` with `heading` (to Anytime) |

Response `{"moved":N}`.

### `POST /api/reorder` (write)

Body `{"view","moved","order"}`. `order` is the new order (2–1000 UUIDs) of the section the item was dragged in; `moved` is the dragged item.

| `view` | Items | Position written |
| --- | --- | --- |
| `today` | to-dos | `todayIndex`; the to-do also takes over its neighbour's Today day (`todayDate`) when it differs. |
| `projects` | projects in the sidebar | `index` |
| `areas` | areas in the sidebar | `index` (batch `area-reorder`) |
| any other value | to-dos in a list or project | `index` |

The moved item gets a position between its neighbours (gaps of 512). Only when there is no room is the section renumbered. All operations go into one `things-cli batch` call. Response `{"reordered":true,"writes":N}`.

### `POST /api/project-area` (write)

Body `{"uuid","area"}`. Moves the project into the area (batch `move`), or out of any area when `area` is `""` (batch `unfile`). Response `{"ok":true}`.

## Client errors

### `POST /api/client-error`

The page reports JavaScript errors and failed requests here. Body `{"message","source","line","stack","context","page"}`. The server logs at most 60 reports per minute as `msg="client error"`, with the account redacted and fields clipped. Response `204`. Not affected by `APP_READ_ONLY`.

## Passkey endpoints (`APP_AUTH=passkey`)

| Route | Purpose |
| --- | --- |
| `GET /login` | Login page (`public/login.html`). `/login?account` shows the account page (add a passkey, sign out). |
| `GET /auth/status` | `{"mode":"passkey","authenticated":bool,"registered":bool,"passkey":"<name>"?}` |
| `POST /auth/login/begin` | Starts a discoverable login: `{"ceremony","options"}`. |
| `POST /auth/login/finish` | `{"ceremony","credential"}`; sets the session cookie: `{"authenticated":true}`. |
| `POST /auth/register/begin` | `{"setupCode"?,"name"?}`. Needs a session or a valid setup code: `{"ceremony","options"}`. |
| `POST /auth/register/finish` | `{"ceremony","credential"}`: `{"registered":true,"name","id"}`. With a setup code, it also signs in. |
| `POST /auth/logout` | Ends the session: `{"authenticated":false}`. |

The relying party is chosen by the request's `Origin`; unknown origins get `403` "Passkeys are not enabled for this address." Failed sign-ins, failed registrations and wrong setup codes count towards the lockout (`429` with `Retry-After`).

## MCP endpoint (`/mcp`)

`POST /mcp` speaks the [Model Context Protocol](https://modelcontextprotocol.io) over Streamable HTTP, so a coding agent such as Claude Code on another machine can read, create, change, order and complete the to-dos of chosen projects, including their tags, notes and checklists (subtasks). It cannot delete, reopen, or touch projects, areas, headings or to-dos outside the token's projects.

**Authentication.** `Authorization: Bearer <token>`, where the token was created on the server with `nubererum mcp-token-create` (see [Deployment — MCP tokens](DEPLOYMENT.md#mcp-tokens-for-coding-agents)). Session cookies and Basic Auth are never accepted on `/mcp`, and MCP tokens are not accepted anywhere else. A missing or unknown token gets `401` with `WWW-Authenticate: Bearer`; unknown tokens count towards the same lockout as failed sign-ins (`429` with `Retry-After`) and are logged with the client address.

**Transport rules.** Stateless: every request is answered with one `application/json` JSON-RPC response (no SSE stream, no `Mcp-Session-Id`). `GET` and `DELETE` get `405`. Batches get `400`. Notifications (no `id`) get `202`. Bodies must be `application/json` (`415`) and at most 64 KiB (`413`). A request with an `Origin` header whose host is neither the request host nor in `APP_PUBLIC_ORIGINS` gets `403`; a request a proxy received over plain HTTP gets `400` instead of a redirect, because the token would already have been sent in clear. Supported protocol versions: `2024-11-05`, `2025-03-26`, `2025-06-18`.

**Methods.** `initialize`, `ping`, `tools/list`, `tools/call`. Tool failures are JSON-RPC errors with the matching HTTP status and `error.data.status` (`401` is reserved for the token; a server without a signed-in Things account answers `503`, a missing `things-cli` `424`). A successful call returns the result both as `structuredContent` and as JSON text in `content[0].text`.

Read tools (every token):

| Tool | Arguments | Result |
| --- | --- | --- |
| `list_projects` | — | `{"projects":[{"uuid","title","note","area":{"uuid","title"}\|null,"when","tags","openCount"}],"syncedAt"}`: the open projects the token may access. |
| `list_areas` | — | `{"areas":[{"uuid","title","tags"}],"syncedAt"}`: the areas carrying one of the token's tags, where it may create projects. |
| `list_tags` | — | `{"tags":[{"uuid","title"}]}`: all Things tags, for `create_todo`/`update_todo`. |
| `list_todos` | `{"project","include_completed"?}` | `{"project":{"uuid","title"},"headings":[{"uuid","title"}],"todos":[{"uuid","position","title","note","checklist":[{"uuid","title","done"}],"heading":{"uuid","title"}\|null,"when","deadline","tags","status"}],"syncedAt"}` in Things order (`position` 1 = top). `when` is `inbox`, `anytime`, `someday` or the start date. Without `include_completed`, completed and canceled to-dos (not yet logged) are left out. |

Write tools (not offered by read-only tokens; refused with `APP_READ_ONLY=1`; together limited to 120 calls per token and hour, `429`). They use the same `things-cli` operations and model patches as the web endpoints. Things Cloud accepts one write per item and commit, so a tool that changes several fields of one to-do runs them as successive batches; if one fails, the error names the writes already applied, the failed ones and the skipped ones, and a sync is started.

| Tool | Arguments | Does |
| --- | --- | --- |
| `create_project` | `{"area","title","note"?,"tags"?,"create_missing_tags"?,"when"?}` | Like `POST /api/create` with `type: project` in an area from `list_areas`; `tags` in a second batch. `when` is `anytime` (default), `someday` or a start date. The new project is in the token's scope through its area. Returns `{"created":true,"project":{…}}`. |
| `update_project` | `{"uuid","title"?,"note"?}` | Renames an accessible project and/or replaces its notes via `things-cli edit`, like `POST /api/update`. |
| `complete_todo` | `{"uuid"}` | Like `POST /api/complete`: the to-do stays in its list, ticked, until it is logged, and can be reopened in the web app. An already completed to-do returns `changed: false` without a write. Returns `{"uuid","title","status","changed"}`. |
| `create_todo` | `{"project","title","note"?,"heading"?,"tags"?,"create_missing_tags"?,"when"?,"deadline"?,"checklist"?,"position"?}` | Like `POST /api/create` plus the details in follow-up batches (`tags`, `deadline`, `checklist-add`, `reorder`). `tags` are titles (case-insensitive); unknown ones are an error unless `create_missing_tags` is set, which creates them like `/api/tag-create`. `when` is `today`, `anytime` (default), `someday` or a start date `YYYY-MM-DD`. `checklist` lists subtask titles. `position` is `bottom` (default) or `top` of the project. Returns `{"created":true,"todo":{…}}`. |
| `update_todo` | `{"uuid","title"?,"note"?,"tags"?,"create_missing_tags"?,"when"?,"deadline"?,"heading"?}` | Only the given fields change: title and note via `things-cli edit`, the rest as one write each (`tags` replaces all tags, `schedule`, `deadline` with `""` to remove, `move` to a heading of the same project or `""` out of it). Returns `{"updated":true,"todo":{…}}`. |
| `reorder_todo` | `{"uuid","before"?\|"after"?\|"position"?}` | Moves the to-do before or after another to-do of the same project (joining that one's heading section), or to the `top`/`bottom` of its section, with `planReorder` like `POST /api/reorder`. Exactly one target. Returns `{"reordered":true,"writes","todo"}`. |
| `add_checklist_items` | `{"todo","titles"}` | Appends subtasks in order (`checklist-add`). Returns `{"ok":true,"writes","todo","checklist"}`. |
| `update_checklist_item` | `{"todo","item","title"?,"done"?}` | Renames and/or ticks a subtask (`checklist-edit`, `checklist-complete`). |
| `remove_checklist_item` | `{"todo","item"}` | Deletes a subtask (`checklist-delete`). |
| `reorder_checklist` | `{"todo","order"}` | Sets the order of all subtasks (`checklist-reorder`); `order` lists every item UUID once. |

Text limits as in the web endpoints: titles 500, notes 5000, checklist items 500 characters, nothing starting with `--`. Headings must belong to the to-do's project (`400` otherwise); a neighbour in `reorder_todo` must be in the same project and accessible to the token.

A token's scope is a list of project UUIDs, a list of Things tags, or both. A tag grants every open project that carries it and every project of an area that carries it (matched case-insensitively, `#` ignored); a tagged area also allows `create_project` there. Tag scopes follow the model, so tagging a project or area in Things adds it after the next sync. Authorization is checked on every call against the server's cached model: `list_todos` needs an allowed project; `complete_todo` needs a to-do whose project (through its heading, if any) is allowed. Unknown and foreign to-dos get the same `403`, so a token cannot probe which UUIDs exist. A to-do missing from the cache is looked up once more after a fresh sync. `create_todo` needs an allowed project, `create_project` a tagged area, `update_project` an allowed project; the other write tools need a to-do in an allowed project. Read-only tokens, `APP_READ_ONLY=1` and more than 120 write calls per token and hour refuse the write tools (`403`, `403`, `429`). Every tool call is logged with the token name, tool and client address (never the token).

## Static files

Paths outside `/api/` (and, in passkey mode, `/auth/` and `/login`) are served from `public/`: `index.html`, `app.js`, `shortcuts.js`, `theme.js`, `styles.css`, `login.html`, `login.js`, `manifest.webmanifest` and the icons. Paths that leave `public/` get `403`. Unknown paths fall back to `index.html`.

## Status codes

| Status | When |
| --- | --- |
| `400` | Invalid body or field. |
| `401` | No Things account signed in; no passkey session; Basic Auth missing or wrong; MCP token missing or unknown. |
| `403` | Read-only instance; login or logout with a server-managed account; same-origin guard; static path outside `public/`; unknown passkey origin; wrong setup code; MCP tool on a project the token may not access, or `complete_todo` with a read-only token. |
| `404` | Unknown API route. |
| `405` | Method other than `GET`, `HEAD` or `POST`; `/mcp` with a method other than `POST`. |
| `409` | The cached model no longer has a referenced item; refresh. Also: the change targets the template of a repeating to-do (shown in Upcoming as "Repeating"), which only takes a new title, notes and the Trash. |
| `424` | `things-cli` not found. |
| `413` | `/mcp` body over 64 KiB. |
| `415` | Write or `/mcp` request without `Content-Type: application/json`. |
| `429` | Too many failed sign-in attempts or unknown MCP tokens; more than 120 MCP write calls per token and hour. |
| `500` | Unexpected error or recovered panic. |
| `502` | `things-cli` failed or printed non-JSON output. Write errors start with "Things Cloud write failed:". |
