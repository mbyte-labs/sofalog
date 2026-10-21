# Sofalog

App to log your shows, movies, watchlist and games.

Self-hosted and multi-user: a single Go binary with an embedded Svelte PWA and a SQLite database,
behind a reverse proxy. Your library — metadata *and* artwork — is stored on your own server, so it
keeps working even when a metadata provider doesn't.

**[SPEC.md](SPEC.md)** is the full specification: stack, architecture, the two components
(movies & TV, games), data model, milestones and scope.
**[docs/architecture.md](docs/architecture.md)** is a visual overview — diagrams of the runtime
shape and the code structure.

> Status: M4 (games) in progress; M0–M2 shipped. See [SPEC.md §8](SPEC.md#8-milestones) for the milestone plan.

## Repository layout

```
server/     Go module (mbyte.ro/sofalog) — the API and, in production, the embedded SPA
frontend/   Svelte 5 + Vite SPA — built to static files that get embedded into the Go binary
api/        openapi.yaml, the contract for /api/v1
docs/       architecture overview and diagrams
data/       runtime volume (SQLite DB, artwork, provider cache) — created at runtime, never committed
```

## Prerequisites

- **Go 1.23+** — builds and runs the backend
- **Node.js 20+ with npm** — builds the frontend (not needed in production; the binary embeds the
  built SPA and no Node runtime is deployed)

## First-time setup

```bash
git clone <repo-url> && cd sofalog
cd frontend && npm install
```

The Go side needs no install step — `go run` / `go build` fetch modules automatically.

## Configuration

All configuration is environment variables, read once at startup. Startup **fails fast** and lists
every problem at once if something required is missing.

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `BASE_URL` | **yes** | — | The absolute external URL users reach the app at (e.g. `https://media.example.com`). The single source of truth for absolute links, the PWA manifest, OIDC redirect URIs and the cookie `Secure` flag — never derived from request headers. For local work use `http://localhost:8080`. |
| `LISTEN_ADDR` | no | `:8080` | Address the HTTP server listens on. Plain HTTP only — TLS is the reverse proxy's job. |
| `DATA_DIR` | no | `./data` | Runtime volume: the SQLite database, downloaded artwork (`assets/`) and the provider cache (`cache/`). Created if missing. |
| `DATABASE_URL` | no | *(empty)* | Reserved for Postgres. Not supported yet — leave unset to use SQLite at `DATA_DIR/sofalog.db`. |
| `TMDB_API_KEY_V3` | no | — | TMDB metadata for movies & TV. The **API Key** field on <https://www.themoviedb.org/settings/api>. Sent as an `api_key` query parameter. |
| `TMDB_API_KEY_V4` | no | — | The **API Read Access Token** field on the same page — a JWT starting `eyJ`. Sent as an `Authorization: Bearer` header. |
| `STEAM_API_KEY` | later (M4) | — | Steam game sync. Server-side only. |
| `ENV_FILE` | no | — | Explicit path to an env file, overriding the `.env` search below. A path that does not exist is a startup error. |
| `OIDC_ISSUER` | no | — | Single sign-on issuer URL. Setting this, `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET` **enables OIDC and disables local password login entirely**. Setting only some of the three is a startup error. |
| `OIDC_CLIENT_ID` | with issuer | — | OAuth client id registered with the provider. |
| `OIDC_CLIENT_SECRET` | with issuer | — | Client secret. Server-side only — never sent to the browser and never logged. |
| `OIDC_ADMIN_CLAIM` | no | — | Claim holding groups or roles, e.g. `groups`. Must be set together with `OIDC_ADMIN_VALUE`. |
| `OIDC_ADMIN_VALUE` | no | — | The value in that claim which grants admin, e.g. `sofalog-admins`. Re-read on every sign-in, so removing someone from the group demotes them next time they log in. With neither set, nobody is admin. |
| `OIDC_SCOPES` | no | `openid profile email` | Scopes requested at the authorize endpoint. `openid` is always included. |
| `OIDC_PROVIDER_NAME` | no | `single sign-on` | Label for the sign-in button. |

Database migrations are applied automatically at startup.

### The two TMDB credentials

TMDB issues two credentials, and its settings page shows both. **Set either one — you do not need
both.** They grant the same access: TMDB documents the v4 read access token as *"a single
authentication process that you can use across both the v3 and v4 methods"*, and sofalog only ever
calls v3 endpoints (`/search/movie`, `/movie/{id}`, `/tv/{id}/season/{n}`, `/configuration`), so the
v4 token buys nothing extra here.

- If **both** are set, the v4 token is used — it keeps the secret in a request header rather than in
  the URL.
- Each variable means what it says. A value in the wrong one is **ignored** with a warning naming the
  variable to move it to, rather than being quietly used anyway.
- Neither set means adding movies and shows fails with a 503; browsing what you already track is
  unaffected.

Both are server-side only: never in an API response, never in the SPA bundle, never logged — not even
inside an error message.

### Providing secrets

Nothing secret belongs on a command line or in a committed file. Put the keys in a `.env` at the
repo root — it is gitignored:

```bash
cp env.example .env
```

Two things read that one file: `docker compose` (for `${...}` interpolation) and the server itself.
The server looks for an env file in this order, and the first one that exists wins:

1. `$ENV_FILE`, if set
2. `.env` in the working directory
3. `../.env` — which is what makes `cd server && go run .` pick up the repo-root file

A missing file is not an error. **A real environment variable always beats the file**, so a
container, a systemd unit or a one-off `TMDB_API_KEY_V3=… go run .` overrides it rather than the other
way round. A malformed `.env` is reported at startup alongside any other configuration problem.

The template is named `env.example`, not `.env.example`, so tooling that blocks `.env.*` to protect
secrets does not also block the template.

### Single sign-on (OpenID Connect)

Set `OIDC_ISSUER`, `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET` and the app switches to SSO. This is
deliberately all-or-nothing: **local email/password authentication is disabled entirely** while a
provider is configured — `/auth/login`, first-run setup, user creation and password changes all
refuse, and the login page offers only the provider button. Setting some but not all three is a
startup error rather than a quiet fallback to passwords.

Register this exact redirect URI with the provider:

```
{BASE_URL}/api/v1/auth/oidc/callback
```

It is derived from `BASE_URL` alone — never from request headers — and the server logs the exact
value at startup.

**Admin** comes from an IdP group or role: set `OIDC_ADMIN_CLAIM=groups` and
`OIDC_ADMIN_VALUE=sofalog-admins` (or whatever your provider uses). It is re-evaluated on every
sign-in, so the IdP stays authoritative — removing someone from the group demotes them the next time
they log in. With neither variable set, nobody is granted admin.

**Migrating existing accounts.** An OIDC identity attaches to an existing account by **verified
email** — and since the email is the login identifier, that simply means the addresses match. If
someone's provider address differs from the one they registered with, update it under
*Settings → Users* before switching and they keep their watch history, ratings and watchlist. An
unverified email never links, an address with no match is provisioned on first sign-in, and a
provider that sends no email at all is refused — check the client's `email` scope.

## Running for development

Two processes: the Go API and the Vite dev server. The frontend dev server proxies `/api` to the
backend, so you browse the Vite URL and get live reload.

**Terminal 1 — backend on :8080**

```bash
cd server && go run .
```

With a repo-root `.env` in place that is all you need — `BASE_URL` and your TMDB credential come
from `../.env`. Without one, pass the required variable inline:

```bash
cd server && BASE_URL=http://localhost:8080 go run .
```

At startup the server fetches TMDB's `/configuration` once, which caches the image base URL and
doubles as a key check. Watch for one of these lines:

```
level=INFO msg="tmdb: ready" image_base=https://image.tmdb.org/t/p/ auth="v4 bearer"
level=WARN msg="tmdb: no credential set — adding movies and shows will fail; browsing is unaffected" expected="TMDB_API_KEY_V3 or TMDB_API_KEY_V4"
level=WARN msg="tmdb: ignoring a misplaced credential" problem="TMDB_API_KEY_V3 holds a v4 read access token (a JWT); ignoring it — move the value to TMDB_API_KEY_V4"
level=WARN msg="tmdb: configuration fetch failed, will retry on first use" err="..."
```

The `auth` field on the ready line tells you which credential is actually in use (`v3 api_key` or
`v4 bearer`) — the scheme, never the value.

None of them stop the server. An unreachable or unconfigured provider costs you the ability to add
new items, never the ability to use the library you already have.

**Terminal 2 — frontend on :5173**

```bash
cd frontend && npm run dev
```

Open <http://localhost:5173>. Changes to Svelte code hot-reload; changes to Go code need a restart
of terminal 1.

Quick check that the backend is up:

```bash
curl http://localhost:8080/api/v1/health
```

## Building for production

The production artifact is **one binary**. The frontend is built first so the Go build can embed it
(`server/web/dist` is embedded via `go:embed`):

```bash
cd frontend && npm run build
cd ../server && go build .
```

Then run it (the binary is `server/sofalog`):

```bash
BASE_URL=https://media.example.com DATA_DIR=/var/lib/sofalog ./server/sofalog
```

Deploy it behind a reverse proxy that terminates TLS. HTTPS is required for the PWA service worker.
The bundled Caddyfile is the one M0 deployment file still outstanding.

## Running with Docker

The `Dockerfile` is multi-stage — Node builds the SPA, Go compiles the binary around it, and only
the binary lands in an Alpine runtime image. `docker-compose.yml` runs it with `./data` bind-mounted
and no secret written into the file:

```bash
cp env.example .env
docker compose up -d --build
```

`TMDB_API_KEY_V3` / `TMDB_API_KEY_V4` reach the container from `.env` or from the host environment
(`TMDB_API_KEY_V4=… docker compose up -d`); the compose file only names them. Check what they
resolved to before starting anything:

```bash
docker compose config
```

The container listens on plain HTTP and publishes to `127.0.0.1:8080` only, on the assumption that a
reverse proxy sits in front of it. Set `TRUSTED_PROXIES` to that proxy's address once it does —
until then `X-Forwarded-*` headers are ignored entirely, which is the safe default.

## Quality gates

Run before considering any change done:

```bash
cd server && gofmt -l . && go vet ./... && go test ./...
```

```bash
cd frontend && npm run check && npm run lint
```

`npm run format` applies Prettier fixes; `npm run check` is `svelte-check`.

## Development conventions

The enforceable rules live in [CLAUDE.md](CLAUDE.md) (layering, data-model invariants, local-first
rules); the reasoning behind them is in [SPEC.md Appendix A](SPEC.md#appendix-a--why-these-decisions).
Highlights:

- Code is organised **per feature** on both sides: `server/features/<feature>` and
  `frontend/src/features/<feature>`.
- Every `/api/v1` change lands with the matching [api/openapi.yaml](api/openapi.yaml) update in the
  same commit.
- The SPA build output (`server/web/dist`) is gitignored except a committed stub, and Vite is
  configured to never delete it (`emptyOutDir: false`) — `go build` must work on a fresh clone.
- Adding a Go module or an npm package is a deliberate decision, not a reflex — ask first.
- Never write into `data/` from tooling; it is a runtime volume.

## License

sofalog is free software under the [GNU Affero General Public License v3.0 or later](LICENSE).
