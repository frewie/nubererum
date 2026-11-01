# Deployment Guide

This app is a small self-hosted web UI for Things Cloud. It is designed for a trusted home network, not the public internet.

## What You Need

- Docker with Docker Compose.
- A Things Cloud account.
- Network access from the host to `https://cloud.culturedcode.com`.

## Quick Start

1. Clone the repository onto your server.

2. Copy the environment template:

   ```sh
   cp .env.example .env
   ```

3. Edit `.env`:

   ```sh
   APP_USERNAME=admin
   APP_PASSWORD=use-a-long-random-password
   THINGS_USERNAME=you@example.com
   THINGS_PASSWORD=your-things-cloud-password
   ```

4. Build and start:

   ```sh
   docker compose up -d --build
   ```

5. Open the app:

   ```text
   http://YOUR-SERVER:4173
   ```

6. Sign in with the Basic Auth username/password from `.env`.

The container reads your Things Cloud data directly. No Mac is required.

## Today View

The Today view comes directly from:

```sh
things-cli today
```

`things-cli` lists a task in Today when it has started: its start date is today or earlier in the container's time zone. Set `TZ` in `.env` (default `Europe/Berlin`) so "today" matches your Things devices.

The app builds the other lists from the CLI state cache and limits them to the tasks `things-cli list` returns, which skips completed, canceled and trashed tasks. It uses the cache to add project, area, and heading labels to the Today rows. Today membership always comes from the CLI result; cache ordering markers are not treated as membership.

## Loading And Sync

A sync runs `things-cli list` and `things-cli today`, which takes a few seconds. The server therefore keeps the last model in memory:

- It syncs once at startup when the Things account comes from the environment.
- `GET /api/sync` answers from that model at once. When it is older than 30 seconds, the server starts a background sync and marks the response `refreshing`; the page shows the cached tasks and replaces them when `GET /api/sync?fresh=1` returns.
- Only one sync runs at a time. After a create or complete, the server removes a completed task from the cached model right away and syncs again.
- JSON, HTML, CSS and JavaScript responses are gzip-compressed.

The page completes and adds tasks optimistically and brings a task back if the request fails. It refreshes when it becomes visible again after a minute and every five minutes while open. Working with to-dos, as in Things:

- Click selects a to-do (Shift extends, Ctrl/Cmd adds); double click, `Enter` or a tap on touch screens opens it. The open to-do edits title, notes and checklist in place; changes are saved as you type or leave a field. `Esc` closes it.
- Ticked-off to-dos stay in their list, greyed out, until they are logged: with Things' daily setting at the next day, or with "Log Completed". Ticking again opens them.
- `Delete`/`Backspace` moves the selected to-dos to the Things Trash; the message offers Undo.
- "Appearance" in the sidebar switches between System (follows the device), Light and Dark; each browser remembers its choice.
- Keyboard shortcuts follow [Things for Mac](https://culturedcode.com/things/support/articles/2785159/) (`public/shortcuts.js`; Cmd on a Mac, Ctrl elsewhere): e.g. `Cmd/Ctrl+K` complete, `Cmd/Ctrl+R`/`O` Anytime/Someday, `Ctrl+]`/`[` start date ±1 day, `Ctrl+.`/`,` deadline ±1 day, `Cmd/Ctrl+Shift+M` move, `Cmd/Ctrl+Shift+O` go to, `Cmd/Ctrl+1`–`6` lists, `Space` new to-do below the selection, paste to add one to-do per line. Browsers keep `Cmd/Ctrl+T`, `N`, `Shift+N` and `Shift+T`, so those also work with `Alt`: `Alt+T` starts the selection Today. `?` (or "Shortcuts" in the sidebar) shows the cheat sheet; `Cmd/Ctrl+E` starts the selection This Evening, `Cmd/Ctrl+Alt+K` (or Alt+click on the checkbox) cancels it; creating repeating to-dos and Markdown are not available.
- The open to-do edits when (Inbox, Today, Anytime, Someday or a start date), the deadline and the tags (with suggestions; unknown names create a tag) below the checklist. Checklist items are reordered by their handle.
- Drag a to-do, or several selected ones, onto a project, area or heading, or onto Today, Anytime or Someday in the sidebar. Like Things, moving into a project keeps the schedule (`move` writes `pr`, `ar` and `agr` only); to-dos from the Inbox become Anytime. In a project, dropping onto a row of another section moves the to-do under that heading (or out of it).
- Projects show all their headings, also empty ones; "New Heading" adds one at the end.
- In the sidebar, projects are dragged within an area, onto another area, or between the projects of another area or the top level; areas are dragged into a new order; "New Area" and "New Project" create them.
- Logbook and Trash list the latest 300 completed or trashed entries (`GET /api/logbook`, `GET /api/trash`, `?limit=` up to 2000). Ticking in the Logbook reopens a to-do; the Trash restores. Emptying the Trash stays in Things.
- On touch screens a long press selects a to-do (taps then add to the selection, which is dragged by its handle) or starts dragging a project or area in the sidebar.

The app keeps one `things-cli serve` process running and sends it every command (set `THINGS_CLI_WORKER=0` to start one process per command instead). Requests taking 3 s or more are logged as `slow request`, CLI commands taking 2 s or more as `slow things-cli command`.

Writes to Things Cloud run one at a time: Things Cloud accepts a write only on top of the latest history state and answers parallel ones with 409 `OutdatedAncestor`. A write is retried up to three times when it certainly did not arrive (that 409, a failed name lookup, a refused connection), never after a timeout. Syncs retry once after a failed lookup.

The sidebar lists projects and areas in Things' order and only active projects; Someday and Upcoming projects appear at the top of those lists. Every list, project and area accepts new to-dos (Upcoming asks for a start date), and "New Project" creates a project in an area. Areas that no to-do or project ever referenced are hidden, because the CLI cache keeps some areas that were deleted in Things.

Drag a to-do to reorder it within its section (mouse: the row; touch: the handle on the right). The server gives it a position between its neighbours, renumbers the section when there is no room, and in Today lets it take over the neighbour's Today day, because Today sorts by that day first. All changes go to Things Cloud in one `things-cli batch` request.

## Logs

The app logs key=value lines to stderr (`level=ERROR msg="request failed" method=POST path=/api/create status=502 response=…`): every failed API or sign-in request, failed syncs and Things Cloud writes, panics, and errors the page reports from the browser (`msg="client error"`). The Things account email and password are replaced by `<account>`. `docker logs nubererum` shows them; ship container logs to your log system to search for `level=ERROR` (or `level=WARN`).

Set `APP_READ_ONLY=1` on test instances that use a real Things account: create and complete then return `403` without running `things-cli`.

## Patched Things CLI

Outside Docker, build the same CLI into `bin/things-cli` with `scripts/build-things-cli.sh`; `scripts/start-nubererum.sh` does this on first start.


The image builds `things-cli` from the original SDK, [`arthursoares/things-cloud-sdk`](https://github.com/arthursoares/things-cloud-sdk), at release v0.6.0 (the Dockerfile checks the tag's commit), and applies `patches/things-cloud-sdk-arthursoares.patch`. v0.6.0 already reads `Task7`, migrates legacy identifiers and clears fields sent as `null`. The patch adds what the app still needs:

- `Task2` tasks and `Tag2` tags from old histories are read. Without this, a history containing `Task2` stops the sync with "unsupported task kind".
- Today also lists tasks whose start date has passed, using the local calendar day, and canceled tasks are no longer listed.
- Tasks keep `IsRepeatingTemplate` and `RepeatRule` (`rr`), `Evening` (`sb`) and `TodayIndexReferenceDate` (`tir`), and areas keep `Index` (`ix`), so the app can follow Things' list rules and order.
- The replay version is raised to 4, so caches from v0.6.0 are rebuilt.
- Deletes of areas, checklist items and tags that Things writes with an empty payload are applied (v0.6.0 skipped them, so deleted areas and checklist items stayed in the cache).
- The cache keeps Things' Logbook settings (`ld`, the time completed to-dos were last logged, and `li`, the logging interval), so the app can show completed to-dos until they are logged; the replay version is 8 (cache `things-cli-state-v8.json`; version 7 added area tags, `tg` of Area3, which the MCP token scope uses; version 8 added `Evening` (`sb` 1, This Evening) and `RepeatRule` (the `rr` object as Things wrote it)).
- `batch` also accepts `create` with `index`/`todayIndex` (to start at the top), `reopen`, `restore` (out of the Trash), `move` (into a project, area or heading, schedule unchanged), `unfile` (out of any project and area), `schedule` (a list or a start date), `deadline` (set or remove), `tags`, `create` with `repeat` (a template with a validated `rr`), `reminder` (`ato` with `st`/`sr`/`tir`, or `ato:null`), `cancel` (`ss:2` with `sp`), `delete` (a heading or a Trash item, Task7 action 2 `{}`; next to deletions, tombstones must name an item deleted in the same commit), `area-reorder`, `area-rename`, `area-tags` (Area3 `{tg}`), `area-delete` (Area3 action 2 `{}`), `tag-rename` (Tag4 `{tt}`), `tag-delete` (Tag4 action 2 `{}`), `log-completed` (sets `ld` on the account settings `RhAzEf6qDxCD5PmnZVtBZR`, as Things' "Log Completed" does) and `checklist-add`, `checklist-edit`, `checklist-complete`, `checklist-reorder` and `checklist-delete` (`ChecklistItem3`). Each writes the fields Things itself writes for that change, as seen in the account history.
- `batch` accepts `{"cmd":"reorder","uuid":…,"index":N}` (position in lists and projects, `ix`) and `{"cmd":"reorder","uuid":…,"todayIndex":N,"todayDate":"YYYY-MM-DD"}` (position in Today, `ti`, and the day the to-do entered Today, `tir`). The Task7 write check accepts `ti` with `tir` alone, which is what Things itself writes when a to-do is dragged in Today (observed in the history on 2026-10-27; reorders in lists write `ix` alone).

To change the patch:

```sh
git clone --branch v0.6.0 https://github.com/arthursoares/things-cloud-sdk.git /tmp/sdk
cd /tmp/sdk && git apply ~/Projects/nubererum/patches/things-cloud-sdk-arthursoares.patch
# edit, then test:
docker run --rm -v /tmp/sdk:/src -w /src golang:1.26-alpine sh -c 'gofmt -l . ; go vet ./... && go test -count=1 ./...'
git add -A && git diff --cached > ~/Projects/nubererum/patches/things-cloud-sdk-arthursoares.patch
```

## Updating

```sh
git pull
docker compose up -d --build
```

## Data Storage

The `things-data` Docker volume stores the Things Cloud CLI state cache:

```text
/data/things-cli-state-v8.json
```

This cache contains task metadata from your Things account. Treat it as private.

## Passkey Sign-In

For internet access, replace Basic Auth with passkeys (WebAuthn). With `compose.yaml`, set them in `.env` (`APP_AUTH=passkey`, `APP_PUBLIC_ORIGINS=https://things.example.com`) and leave `APP_PASSWORD` out; in your own Compose file:

```yaml
environment:
  APP_AUTH: "passkey"
  APP_PUBLIC_ORIGINS: "https://things.example.com"  # the one origin passkeys are bound to
```

Every entry of `APP_PUBLIC_ORIGINS` (or of `APP_PASSKEY_ORIGINS`, if set) is its own relying party, with its host name as RP ID. Origins must be `https`, except `http://localhost[:port]` for tests. A passkey works only on the origin it was registered on, so register one per origin (the first one on each origin needs a setup code). Passkeys stored without an RP ID belong to the first origin. `APP_PASSWORD` is ignored in this mode, and there is no password fallback.

Register the first passkey (and recover after losing all of them) with a one-time setup code created on the server:

```sh
docker exec nubererum nubererum passkey-setup   # prints a code valid once for 30 minutes
```

Open `/login`, expand "Register a passkey with a setup code", enter the code and confirm the passkey prompt. Signed in, the "Passkeys" link in the sidebar adds more passkeys without a code. Manage passkeys on the server:

```sh
docker exec nubererum nubererum passkey-list
docker exec nubererum nubererum passkey-remove <id or first 6+ characters>
```

Removing a passkey ends its sessions. Passkeys are stored in `/data/passkeys.json` (mode 0600), the hashed setup code in `/data/passkey-setup.json`. Sessions live in memory, so a restart signs everyone out.

Test the passkey mode in a throwaway container before deploying:

```sh
docker build -t nubererum:passkey-test .
docker run -d --name things-passkey-test -p 127.0.0.1:3007:4173 --read-only \
  --tmpfs /data:uid=10001,gid=10001,mode=700 --tmpfs /tmp \
  -e APP_AUTH=passkey -e APP_PUBLIC_ORIGINS=http://localhost:3007 nubererum:passkey-test
PASSKEY_CONTAINER=things-passkey-test npx playwright test tests/passkey.spec.js
docker rm -f things-passkey-test
```

## MCP Tokens for Coding Agents

`POST /mcp` lets Claude Code (or another MCP client) on another machine read, create, change, order and complete the to-dos of chosen projects, with tags, notes and checklists (subtasks), and create projects in tagged areas; nothing outside those projects and areas, no deleting (see [API — MCP endpoint](api.md#mcp-endpoint-mcp)). Passkeys cannot be used from a CLI, so the endpoint has its own tokens, managed on the server like passkeys. ```sh
docker exec nubererum nubererum mcp-token-projects   # open projects with their UUIDs and tags
docker exec nubererum nubererum mcp-token-create --name laptop --tag Coding [--read-only]
docker exec nubererum nubererum mcp-token-create --name repo-a --project <project uuid> [--project <uuid> ...]
docker exec nubererum nubererum mcp-token-list
docker exec nubererum nubererum mcp-token-remove laptop
```

A token's scope is given by `--tag` (every open project that carries the Things tag, and every project of an area that carries it; in such an area the token may also create projects), by `--project` UUIDs, or both. Tag a project or area in Things to add it, remove the tag to take it away. `mcp-token-create` prints the token once; it is stored only as a hash in `/data/mcp-tokens.json`. `--read-only` leaves out `complete_todo`. Removing a token refuses its next request. The usual setup is one token per machine scoped by a tag such as `Coding`, or one token per repository limited to that repository's project.

### Client setup: Claude Code

On the client machine, keep the token in a file only the user can read and export it from the shell profile, so it never lands in a config file or repository:

```sh
umask 077; mkdir -p ~/.config/nubererum
printf '%s\n' 'tca_...' > ~/.config/nubererum/mcp-token
# in ~/.bash_profile, ~/.bashrc or ~/.zshrc:
export THINGS_MCP_TOKEN="$(cat ~/.config/nubererum/mcp-token 2>/dev/null)"
```

Register the server once for the user (all repositories on the machine); Claude Code expands `${THINGS_MCP_TOKEN}` when it starts:

```sh
claude mcp add -s user --transport http things https://things.example.com/mcp \
  --header "Authorization: Bearer \${THINGS_MCP_TOKEN}"
claude mcp list   # "things: ... - ✓ Connected"
```

For a repository instead, commit a `.mcp.json` with the placeholder and never the token:

```json
{ "mcpServers": { "things": { "type": "http", "url": "https://things.example.com/mcp",
  "headers": { "Authorization": "Bearer ${THINGS_MCP_TOKEN}" } } } }
```

Then tell the agent in the repository's `CLAUDE.md` which Things project belongs to it, e.g.:

```markdown
## Things
The open to-dos of this repo are in the Things project "RailMap" (`Fj3NZ…`).
Read them with the `things` MCP server (`list_todos`, in order); when a to-do is
done and verified, mark it with `complete_todo`. New work goes in with
`create_todo` (tag `Coding`, subtasks as `checklist`); keep notes and order
current with `update_todo` and `reorder_todo`.
```

The tools appear as `mcp__things__<tool>`; allow the read tools (`list_projects`, `list_tags`, `list_todos`) in `.claude/settings.json` (`permissions.allow`) and leave the write tools to ask, if you want to confirm every change.

### Client setup: OpenCode

OpenCode reads `~/.config/opencode/opencode.json` (or `opencode.jsonc`) and substitutes `{file:…}` and `{env:…}` placeholders:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "things": {
      "type": "remote",
      "url": "https://things.example.com/mcp",
      "oauth": false,
      "headers": { "Authorization": "Bearer {file:~/.config/nubererum/mcp-token}" },
      "enabled": true
    }
  }
}
```

The same `CLAUDE.md` hint works in `AGENTS.md`, which OpenCode reads.

The endpoint needs HTTPS (plain-HTTP requests are refused, not redirected), works in both auth modes, and is subject to `APP_READ_ONLY`.

## Security Recommendations

- Keep the repository private.
- Use passkeys (`APP_AUTH=passkey`) when the app is reachable from the internet; otherwise use a long, unique `APP_PASSWORD`.
- Do not expose this app directly to the internet; put it behind a TLS reverse proxy.
- Put it behind a VPN such as Tailscale or WireGuard for remote access.
- Keep `.env` out of Git. Keep MCP tokens out of repositories; pass them through environment variables.
- Restrict the Docker host to trusted users.

## Troubleshooting

Check logs:

```sh
docker compose logs -f nubererum
```

Rebuild from scratch:

```sh
docker compose down
docker compose up -d --build
```

If sync fails, verify that the host can reach Things Cloud:

```sh
docker compose exec nubererum /usr/local/bin/things-cli today
```

