# Changelog

All notable changes to NubeRerum. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is its GitHub release notes (`scripts/release-notes.sh`).

## [Unreleased]

### Features

- **Area filter:** Today, Upcoming, Anytime and Someday have an area bar ("All", then the areas of the list's to-dos). Choosing an area shows only its to-dos, including those in its projects; each list remembers its choice in the browser, also after a reload. New to-dos added while an area is chosen go to that area.

### Fixes

- To-dos created under a heading over MCP (`create_todo` with `heading`) showed without their heading in the web app, while Things showed them under it. The web app now reads them like Things does, and new ones are written like Things writes them (the heading only, no separate project).

## [0.2.0] - 2026-11-02

### Features

- **Appearance:** System, Light and Dark themes for the app and the sign-in page. "Appearance" in the sidebar switches between them; each browser remembers its choice, and pages never flash in the wrong theme.
- **MCP headings:** the new tools `create_heading` (a heading at the end of a project, optionally with to-dos filed under it) and `update_heading` (rename). Reconnect your MCP client to see them.
- **MCP tokens in the web app:** "MCP Tokens" in the sidebar lists the tokens with their scope, the projects they reach and their last use, creates tokens (tags and/or projects, optionally read-only; shown once, with the Claude Code command) and removes them. The `mcp-token-*` commands still work.
- **Released images:** every release is published as `ghcr.io/frewie/nubererum:<version>` (and `:latest`) for amd64 and arm64, so you can run a release without building it.

### Fixes

- Token changes and a token's "last used" time can no longer overwrite each other when they happen at the same moment.

### Changes

- The Docker build stage cross-compiles, so multi-platform images build quickly; `docker build .` works as before.
- Releases and the workflow behind them are documented in `AGENTS.md` and `CONTRIBUTING.md`.

## [0.1.0] - 2026-10-31

The first public release.

### Features

- Inbox, Today (with This Evening), Upcoming, Anytime, Someday, Logbook and Trash, with Things' membership and order; a sidebar of areas and projects ordered like Things.
- Create, edit, complete, cancel, reopen, move, reorder and trash to-dos; checklists, notes with a Markdown preview, when, deadline, reminders, tags, repeating to-dos and Convert to Project.
- Projects, headings, areas and tags: create, rename, delete; projects with When, deadline, tags, progress and complete or cancel; areas carry tags; a tag bar filters every list.
- Selection, keyboard shortcuts after Things for Mac with a cheat sheet (`?`), drag and drop with long press on touch screens, search including the Logbook.
- An MCP endpoint (`/mcp`) for coding agents such as Claude Code, with tokens bound to projects or Things tags.

### Security

- Basic Auth with a lockout for LAN use, passkeys (WebAuthn) for internet access, a same-origin guard for writes, HTTPS redirect and security headers, a hardened read-only container; `APP_READ_ONLY=1` refuses every write.

[Unreleased]: https://github.com/frewie/nubererum/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/frewie/nubererum/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/frewie/nubererum/releases/tag/v0.1.0
