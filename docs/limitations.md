# Limitations & Open Questions

What the app does not do, known constraints and open questions. Derived from the source, the tests and the operator docs; where they disagree, the source wins.

## Things Cloud access

- **No official API.** The app depends on the reverse-engineered `things-cli` from `arthursoares/things-cloud-sdk` v0.6.0 plus a local patch (`patches/things-cloud-sdk-arthursoares.patch`). Changes on Cultured Code's side can break reads or writes.
- **The account credentials are on the server.** The Things Cloud email and password must be available to the server process and the CLI (environment or browser sign-in). Things Cloud has no app passwords.
- **One account, no per-user model.** Whoever passes the app's Basic Auth or passkey sign-in operates that account.
- **Writes are only partly verified.** Writes copy the fields Things itself writes, as seen in the account history. They were tested against stand-ins and the SDK's write check, plus some live use by the owner. The CLI has no dry run.
- **The CLI cache is a full copy of the account's task metadata** (`things-cli-state-v8.json`). Keep it private. A new cache format means a new file name and a full rebuild on the next sync.

## Features not offered

- **Repeating to-dos** are made with Repeat… (`Mod+Shift+R`): a template replaces the to-do (the original, with its checklist, goes to the Trash), and the Things apps create the to-dos from the first day on, so nothing appears until a Things device has synced. Only the rules Things itself wrote in the owner's history are offered: every N days, weeks, months (same day, last day, or the same weekday in weeks 1–4) or years, on a schedule or after completion, without an end date, deadline or early start. A rule cannot be changed; trash the template to stop it. The to-dos a series creates (instances) can be completed, moved, rescheduled and edited like any other. The template, shown in Upcoming as "Repeating", only takes a new title, notes and the Trash. Completing an instance of an "after completion" series in the app relies on a Things client to schedule the next one, as when another device completes it.
- **Reminders ring only in the Things apps.** The app sets and removes them (they need a date: Today, This Evening or a start date) and shows a bell with the time, but the browser does not notify.
- **Markdown in notes is a preview.** The editor shows the Markdown Things renders (headings, lists, bold, italic, code, links) below the plain notes field, not in place; Things' other note formatting is not rendered.
- **Emptying the Trash cannot be undone**, as in Things. Checklist items of deleted to-dos stay in the history, as Things leaves them.
- **Logbook and Trash show 300 recent entries at first**; "Show more" loads 300 more, up to 2000. Search finds at most 100 logged entries.
- **`POST /api/create` does not take `when: upcoming`.** Use a future `scheduled` date; the page asks for one in Upcoming.

## Sync behaviour

- **Lists can be up to 30 seconds old** when they first appear. `GET /api/sync` answers from the server's cached model and refreshes in the background; the page then asks for a fresh model.
- **Today membership is decided by `things-cli today`.** Cache fields only order the rows. Completed to-dos not yet logged stay in their lists; the rule follows the cached Things logbook settings (`ld`, `li`).
- **The day boundary follows `TZ`.** If the container's time zone differs from your Things devices, Today and Upcoming can differ.

## Security controls and their bounds

- **No TLS in the app.** Use a TLS reverse proxy (it must set `X-Forwarded-Proto` and `X-Forwarded-For`) or a VPN. See [Security](SECURITY.md).
- **Basic Auth has no second factor.** Use `APP_AUTH=passkey` for internet access.
- **Passkey sessions live in memory.** A restart signs everyone out.
- **`GET /api/sync` shows the full CLI path** in `source`; `/api/status` shows only the base name.
- **`backend.directCloud` and `backend.nativeFallback` are constants** (`true`, `false`) in `/api/status`. There is no reader without `things-cli`; without it, routes that need the CLI return `424`.
- **The static handler is simple.** It contains requests to `public/` and falls back to `index.html` for unknown paths.

## Deployment

- **No automatic updates.** Update with `git pull` and a rebuild (see [Deployment](DEPLOYMENT.md)).

## Open questions

- Will Cultured Code publish an API or app passwords that could replace the account password?
- How stable is the `things-cli` JSON output and state format across SDK releases? The app reads rows as loose JSON objects, so renamed fields would degrade quietly.
- Should `GET /api/sync`'s `source` field be reduced to the base file name, as in `/api/status`?
