# Security Notes

## Trust Boundary

The app talks to Things Cloud through `things-cli` and serves a local web UI. The sensitive assets are:

- Things Cloud email and password.
- The local Things CLI cache.
- Ability to create Inbox tasks and complete tasks through the web UI.
- MCP tokens, which let a coding agent read, create, change, order and complete the to-dos of chosen projects.

## Built-In Controls

- `APP_AUTH=passkey` replaces Basic Auth with WebAuthn passkeys for one owner. Credentials must be discoverable (resident key) with user verification, and are bound to the origin they were registered on. Each origin in `APP_PASSKEY_ORIGINS` or `APP_PUBLIC_ORIGINS` is a separate relying party (https only, except `http://localhost`), and a ceremony must start and finish on the same origin. There is no password fallback.
- In passkey mode, a session is a random 32-byte token in the cookie `__Host-things_session` (`Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`), stored hashed in memory for 30 days. A session ends when its passkey is removed. Unauthenticated `/api/*` calls get `401`, pages redirect to `/login`.
- Registering a passkey needs a session or a one-time setup code from `nubererum passkey-setup`. The code has 80 random bits, is stored as a SHA-256 hash in `/data/passkey-setup.json`, expires after 30 minutes and is consumed by the registration. Passkeys are stored in `/data/passkeys.json` (0600).
- Failed passkey sign-ins, failed registrations and wrong setup codes count towards the same lockout as Basic Auth failures. Each WebAuthn ceremony can be finished once within 5 minutes. A signature counter that goes backwards (a possibly cloned authenticator) is rejected.
- `POST /mcp` (for coding agents) accepts only Bearer tokens created with `nubererum mcp-token-create`; a session cookie or Basic Auth never authenticates it, and an MCP token authenticates nothing else. Tokens are 32 random bytes with a `tca_` prefix, stored as SHA-256 hashes in `/data/mcp-tokens.json` (0600) and shown once at creation. Each token is bound to a list of project UUIDs and/or Things tags (every open project carrying one of the tags, and every project of an area carrying one, where the token may also create projects), optionally read-only; every call is checked against the server's cached model, and unknown and foreign to-dos get the same `403`. Unknown tokens count towards the sign-in lockout. The endpoint is stateless (no MCP session), accepts `POST` only, rejects foreign `Origin` headers, refuses plain-HTTP requests instead of redirecting them, limits bodies to 64 KiB and the write tools to 120 calls per token and hour, and logs every tool call with the token name and client address. The write tools (create, update, reorder, checklist, complete) stay inside the token's projects, reuse the web endpoints' validation and `things-cli` operations, and cannot delete, reopen, create or change areas or headings, or move projects between areas (projects can be created in tagged areas and renamed); `create_missing_tags` is the only way a token adds a tag. `APP_READ_ONLY=1` refuses every write tool.
- Docker deployments require `APP_PASSWORD`.
- The server defaults to `127.0.0.1` unless `HOST` is configured.
- The Docker Compose file sets `HOST=0.0.0.0` only inside the container and requires Basic Auth.
- The app refuses to start on a non-loopback `HOST` without an `APP_PASSWORD` of at least 16 characters.
- Failed Basic Auth attempts are limited per client: 5 failures in 15 minutes block the client for 15 minutes (`429`). After 50 failures from all clients within 15 minutes, a single failure blocks a client. Failures and blocks are logged with the client address.
- The client address is the rightmost `X-Forwarded-For` entry, which the nearest proxy appends and clients cannot forge.
- Requests a proxy received over plain HTTP (`X-Forwarded-Proto: http`) are redirected to HTTPS before credentials are requested. HTTPS responses carry HSTS.
- State-changing requests must be `POST` with `Content-Type: application/json`, an `Origin` or `Referer` matching the host (or `APP_PUBLIC_ORIGINS`), and no cross-site `Sec-Fetch-Site`. Bodies are limited to 64 KiB.
- When the Things account comes from the environment, `/api/login` and `/api/logout` are disabled, so the browser can neither switch nor clear the account.
- Task, project, heading and area IDs are validated, and titles or notes starting with `--` are rejected, so input cannot be read as `things-cli` options.
- API responses are `Cache-Control: no-store`. Responses carry CSP, frame, referrer, permissions and cross-origin isolation headers.
- The HTTP server sets header, read, write and idle timeouts.
- The Compose file runs the container read-only, without Linux capabilities, with `no-new-privileges`, and with process, memory and log limits.
- Credentials are passed to `things-cli` through process environment variables, not browser storage.
- The Things CLI cache path is configurable and stored in the Docker volume.

## Known Limitations

- Things Cloud does not have an official public API.
- Things Cloud credentials must be available to the server process.
- There is no per-user authorization model inside the app. MCP tokens are the only scoped credential, and their scope is a list of projects.
- To-do titles, notes and checklists returned over `/mcp` are the owner's own text; a coding agent reading them should treat them as data, not instructions. The server's `initialize` instructions say so.
- A token stored in a client's MCP configuration is only as safe as that machine; store it in an environment variable (`${THINGS_MCP_TOKEN}`) rather than in a repository's `.mcp.json`, use one token per repository, and remove tokens that are no longer used (`mcp-token-list` shows the last use).
- Basic Auth has no second factor; use passkey mode for internet access. When the app is reachable from the internet, it must sit behind a TLS reverse proxy (with HTTP redirected to HTTPS), and `APP_PASSWORD` should be a long random value.

## Do Not Commit

- `.env`
- `passkeys.json`, `passkey-setup.json`, `mcp-tokens.json`
- `things-cli-state*.json`
- `.things-cli-state*.json`
- `bin/`
