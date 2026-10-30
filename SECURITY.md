# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub: open the repository's **Security** tab and choose **Report a vulnerability** (private security advisory). Do not open a public issue for a vulnerability.

Include what you found, how to reproduce it and what an attacker could do. Never include real Things Cloud credentials, cache files (`things-cli-state*.json`) or passkey files.

## Supported versions

Only the latest commit on the default branch is supported. There are no release branches; fixes land there.

## Scope

The app holds the Things Cloud credentials of its owner and can change their Things data, so reports about authentication (Basic Auth and its lockout, passkeys, sessions, MCP tokens), the same-origin guard, credential handling and log redaction, input validation before the CLI runs, and the static file handler are especially welcome. How the app is secured, and how to run it safely, is described in [`docs/SECURITY.md`](docs/SECURITY.md).
