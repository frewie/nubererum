# Things Parity Testing

This workflow compares the app with Things for Mac using task UUIDs. The reference export omits task titles and notes, but UUIDs and hierarchy are still private account data. Keep generated manifests in `.things-parity/`; that directory is ignored by Git and should be readable only by your user. The comparator requires Node.js 18 or newer.

## 1. Create A Controlled Dataset

Create a small set of temporary canary tasks in Things:

- Two tasks in Today, arranged in a known order.
- Two tasks in Inbox.
- Two tasks in Anytime, arranged in a known order.
- One task in Upcoming.
- One task in Someday.
- One completed task in Logbook that must not appear in any open list. The exporter does not read Logbook, so this task is reported as `unexpected` if it leaks into an open app list.
- At least one task inside a project and one directly inside an area.

Use a unique prefix so the canaries are easy to remove afterward. Wait for Things Cloud to finish syncing before capturing either side.

## 2. Export Things On The Mac

Run this from a checkout of the repository on the Mac:

```sh
umask 077
mkdir -p .things-parity
chmod 700 .things-parity
osascript scripts/export-things-reference.applescript > .things-parity/reference.json.tmp \
  && mv .things-parity/reference.json.tmp .things-parity/reference.json
```

macOS may ask for permission to control Things. The Things app must use English built-in list names for this initial exporter. AppleScript returns each collection in the same order as the Things UI.

The exporter reads only the open lists. Reading the complete Logbook takes many minutes on a mature account, and a completed task that leaks into the app is still caught because it is missing from every open Things list. A hand-built reference may still include `lists.logbook`; the comparator then reports such leaks as `completed-leak`.

If the app runs on another machine, securely transfer `reference.json` into that machine's `.things-parity/` directory.

## 3. Compare The Running App

Sign into the app, then run:

```sh
npm run compare:parity -- --reference .things-parity/reference.json
```

Use `--url` if the app is not at `http://127.0.0.1:4173`:

```sh
npm run compare:parity -- --reference .things-parity/reference.json --url http://<lan-ip>:4173
```

Projects and tasks completed today appear in Things lists but not in the app, so the comparator skips reference items whose `type` is `project` or whose `status` is not `open`. The command exits `0` only when list membership, ordering, open status, project, and directly observable area relationships match; UUIDs must be unique; and no task may appear in an open app list unless Things shows it there. It prints only shortened UUIDs, never titles or notes.

If Basic Auth protects the app, provide it through environment variables rather than putting credentials in the URL:

```sh
export PARITY_APP_USERNAME=admin
read -r -s -p "App password: " PARITY_APP_PASSWORD && printf '\n'
export PARITY_APP_PASSWORD
npm run compare:parity -- --reference .things-parity/reference.json
```

The `read` command above is for Bash and keeps the password out of shell history. Unset it after the comparison with `unset PARITY_APP_PASSWORD`.

## 4. Stabilize Before Diagnosing

If the comparison fails, wait for cloud sync and repeat both exports once. Diagnose only mismatches that remain stable across two captures. Do not edit or complete tasks between the reference export and app comparison.

The AppleScript API does not expose heading data reliably. Heading parity remains covered separately by cache fixtures and should not be inferred from this report.

Inside one Upcoming day, AppleScript returns the to-dos in an order that is not the one Things shows (seen on 2026-10-30: neither the project order, the title nor the creation date), so the comparator checks Upcoming by day: the Things sequence must walk the same days as the app in the same order, and the order inside a day is not compared. Repeating templates sit under their next date (`todayDate` in the app).
