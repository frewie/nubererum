# NubeRerum

A small self-hosted web app for Things 3 tasks in Things Cloud. It runs in Docker on a home server and does not need a Mac that is always running. Cultured Code does not publish a Things Cloud API, so the app drives the reverse-engineered `things-cli` from [`arthursoares/things-cloud-sdk`](https://github.com/arthursoares/things-cloud-sdk) v0.6.0, built with the patch in `patches/things-cloud-sdk-arthursoares.patch`.

## What it does

- Inbox, Today, Upcoming, Anytime, Someday, Logbook and Trash, with Things' list rules and order.
- A sidebar ordered like Things: areas with their projects; Someday and Upcoming projects show in those lists.
- Add to-dos in every list, project and area (at the top); create projects, areas, headings and tags.
- Edit to-dos in place: title, notes, checklist, when, deadline and tags.
- Complete, reopen, log completed, trash and restore to-dos.
- Selection, keyboard shortcuts after Things for Mac with a cheat sheet (`?`), drag and drop (also long press on touch screens).
- Docker Compose deployment. Basic Auth for local and LAN use, passkeys for internet access.

## Documentation map

| Page | What it covers |
| --- | --- |
| [Setup & Commands](setup.md) | Docker Compose, local development, environment variables. |
| [API Reference](api.md) | HTTP routes, request and response shapes, validation, errors. |
| [Architecture](architecture.md) | Server, CLI adapter, model cache, sync, writes, middleware, frontend. |
| [Testing](testing.md) | Go tests, Playwright tests, parity workflow. |
| [Limitations](limitations.md) | Known constraints and open questions. |
| [Deployment](DEPLOYMENT.md) *(operator doc)* | Self-hosted deployment, sync behaviour, passkeys, patched CLI, logs. |
| [Security](SECURITY.md) *(operator doc)* | Trust boundary, controls, known limitations. |
| [Parity Testing](PARITY_TESTING.md) *(operator doc)* | Compare the app with Things for Mac. |

## Authority model

The source, its tests and the operator docs are authoritative. When this site or the generated files in the repository root disagree with them, the source wins. `llms.txt`, `llms-full.txt` and `docs/repository-map.json` are generated summaries of the same sources.

## Key facts

- Listens on `127.0.0.1:4173` by default; set `HOST` and `PORT` to change it.
- On a non-loopback `HOST` in Basic Auth mode, the app refuses to start without an `APP_PASSWORD` of at least 16 characters.
- `APP_AUTH=passkey` switches to WebAuthn passkeys; origins come from `APP_PASSKEY_ORIGINS` or `APP_PUBLIC_ORIGINS`.
- Reads run `things-cli list` and `things-cli today` and use the CLI state cache `things-cli-state-v8.json`. The server keeps the last model in memory and refreshes it in the background.
- Writes go through `things-cli` one at a time. `APP_READ_ONLY=1` rejects all write endpoints with `403`.
- The Docker image builds `things-cli` itself; the cache lives in the `/data` volume.
