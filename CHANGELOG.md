# Changelog

All notable changes to NubeRerum. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release's section is its GitHub release notes (`scripts/release-notes.sh`).

## [Unreleased]

## [0.1.0] - 2026-11-02

The first public release.

### Features

- Inbox, Today (with This Evening), Upcoming, Anytime, Someday, Logbook and Trash, with Things' membership and order; a sidebar of areas and projects ordered like Things.
- Create, edit, complete, cancel, reopen, move, reorder and trash to-dos; checklists, notes with a Markdown preview, when, deadline, reminders, tags, repeating to-dos and Convert to Project.
- Projects, headings, areas and tags: create, rename, delete; projects with When, deadline, tags, progress and complete or cancel; areas carry tags; a tag bar filters every list.
- Selection, keyboard shortcuts after Things for Mac with a cheat sheet (`?`), drag and drop with long press on touch screens, search including the Logbook.
- Appearance: System, Light and Dark, remembered per browser.
- An MCP endpoint (`/mcp`) for coding agents such as Claude Code: list, create, change, order and complete to-dos with checklists, tags and headings, create and rename headings, create projects in tagged areas. Tokens are bound to projects or Things tags, optionally read-only, and managed in the web app ("MCP Tokens") or with `mcp-token-*` commands.
- The Docker image: from this release on also published as `ghcr.io/frewie/nubererum` for amd64 and arm64.

### Security

- Basic Auth with a lockout for LAN use, passkeys (WebAuthn) for internet access, a same-origin guard for writes, HTTPS redirect and security headers, a hardened read-only container; `APP_READ_ONLY=1` refuses every write.

[Unreleased]: https://github.com/frewie/nubererum/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/frewie/nubererum/releases/tag/v0.1.0
