# Configuration

All configuration is environment variables, read once at startup. Startup **fails fast** and lists
every problem at once if something required is missing or malformed. Database migrations are
applied automatically at startup.

The committed template [`env.example`](../env.example) documents every variable with its default;
copy it to `.env` and fill in what you need (see [Providing secrets](#providing-secrets)).

## Reference

| Variable | Required | Default | Purpose |
|---|---|---|---|
| `BASE_URL` | **yes** | — | The absolute external URL users reach the app at (e.g. `https://media.example.com`). The single source of truth for absolute links, the PWA manifest, OIDC redirect URIs and the cookie `Secure` flag — never derived from request headers. For local work use `http://localhost:8080`. |
| `LISTEN_ADDR` | no | `:8080` | Address the HTTP server listens on. Plain HTTP only — TLS is the reverse proxy's job. |
| `DATA_DIR` | no | `./data` | Runtime volume: the SQLite database, downloaded artwork (`assets/`) and the provider cache (`cache/`). Created if missing. |
| `DATABASE_URL` | no | *(empty)* | Reserved for Postgres. Not supported yet — leave unset to use SQLite at `DATA_DIR/sofalog.db`. |
| `TRUSTED_PROXIES` | no | *(empty)* | CIDRs or IPs of the reverse proxy, comma separated. `X-Forwarded-*` headers are honoured only from these peers; empty means they are ignored entirely. |
| `TMDB_API_KEY_V3` | no | — | TMDB metadata for movies & TV. The **API Key** field on <https://www.themoviedb.org/settings/api>. Sent as an `api_key` query parameter. |
| `TMDB_API_KEY_V4` | no | — | The **API Read Access Token** field on the same page — a JWT starting `eyJ`. Sent as an `Authorization: Bearer` header. See [The two TMDB credentials](#the-two-tmdb-credentials). |
| `TMDB_WATCH_REGION` | no | `US` | ISO-3166-1 alpha-2 country whose "where to watch" listing the detail pages show. Snapshots store only this country's listing, so changing it costs each tracked item one automatic refresh on its next view. A value that is not two letters is a startup error. |
| `TMDB_REFRESH_INTERVAL` | no | `7d` | Cadence of the **active** refresh job: every movie and show on any watchlist or favorites list, plus followed shows TMDB still lists as in production. Whole days (`7d`) or a Go duration (`36h`); `0` disables the job; under `1h` or a mixed form like `7d12h` is a startup error. |
| `TMDB_ARCHIVE_REFRESH_INTERVAL` | no | `90d` | Cadence of the **archive** refresh job: watched movies and followed shows TMDB has ended or cancelled, when on no list. Same syntax and rules. |
| `DEFAULT_TIMEZONE` | no | `UTC` | IANA zone for every user who has not picked one on their Account page. It decides where their day ends (which episodes count as aired, what Upcoming calls "Today") and the zone dates are shown in. An unknown name or `Local` is a startup error. Every timestamp is stored in UTC regardless. |
| `JELLYFIN_URL` | with key | — | Absolute http(s) URL of the household's Jellyfin server. Setting it together with `JELLYFIN_API_KEY` turns on the library scan, which lights the Jellyfin tile under "Watch now" for every user; setting only one of the two is a startup error. |
| `JELLYFIN_API_KEY` | with URL | — | An API key from Jellyfin's dashboard (Administration → API Keys). Server-side only. |
| `JELLYFIN_SCAN_INTERVAL` | no | `6h` | How often the scan runs; same syntax as the refresh intervals. `0` leaves only the "Scan now" button under Settings → Jellyfin. |
| `STEAM_API_KEY` | later (M4) | — | Steam game sync, from <https://steamcommunity.com/dev/apikey>. Server-side only. |
| `ENV_FILE` | no | — | Explicit path to an env file, overriding the `.env` search below. A path that does not exist is a startup error. |
| `OIDC_ISSUER` | no | — | Single sign-on issuer URL. Setting this, `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET` **enables OIDC and disables local password login entirely**. Setting only some of the three is a startup error. |
| `OIDC_CLIENT_ID` | with issuer | — | OAuth client id registered with the provider. |
| `OIDC_CLIENT_SECRET` | with issuer | — | Client secret. Server-side only — never sent to the browser and never logged. |
| `OIDC_ADMIN_CLAIM` | no | — | Claim holding groups or roles, e.g. `groups`. Must be set together with `OIDC_ADMIN_VALUE`. |
| `OIDC_ADMIN_VALUE` | no | — | The value in that claim which grants admin, e.g. `sofalog-admins`. Re-read on every sign-in, so removing someone from the group demotes them next time they log in. With neither set, nobody is admin. |
| `OIDC_SCOPES` | no | `openid profile email` | Scopes requested at the authorize endpoint. `openid` is always included. |
| `OIDC_PROVIDER_NAME` | no | `single sign-on` | Label for the sign-in button. |

Every secret here — the TMDB credential, the Steam key, the OIDC client secret, the Jellyfin key —
is server-side only: never in an API response, never in the SPA bundle, never logged, not even
inside an error message.

## The two TMDB credentials

TMDB issues two credentials, and its settings page shows both. **Set either one — you do not need
both.** They grant the same access: TMDB documents the v4 read access token as *"a single
authentication process that you can use across both the v3 and v4 methods"*, and sofalog only ever
calls v3 endpoints, so the v4 token buys nothing extra here.

- If **both** are set, the v4 token is used — it keeps the secret in a request header rather than in
  the URL.
- Each variable means what it says. A value in the wrong one is **ignored** with a warning naming the
  variable to move it to, rather than being quietly used anyway.
- Neither set means adding movies and shows fails with a 503; browsing what you already track is
  unaffected.

At startup the server fetches TMDB's `/configuration` once, which caches the image base URL and
doubles as a key check. One of these lines tells you where you stand:

```
level=INFO msg="tmdb: ready" image_base=https://image.tmdb.org/t/p/ auth="v4 bearer"
level=WARN msg="tmdb: no credential set — adding movies and shows will fail; browsing is unaffected" expected="TMDB_API_KEY_V3 or TMDB_API_KEY_V4"
level=WARN msg="tmdb: ignoring a misplaced credential" problem="TMDB_API_KEY_V3 holds a v4 read access token (a JWT); ignoring it — move the value to TMDB_API_KEY_V4"
level=WARN msg="tmdb: configuration fetch failed, will retry on first use" err="..."
```

The `auth` field on the ready line tells you which credential is actually in use (`v3 api_key` or
`v4 bearer`) — the scheme, never the value. None of them stop the server.

## Providing secrets

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

## Reverse proxy

The server speaks plain HTTP and the Docker container publishes to `127.0.0.1:8080` only. Put a
reverse proxy that terminates TLS in front of it — HTTPS is required for the PWA service worker —
and set `TRUSTED_PROXIES` to the proxy's address so the real client IP reaches the session table.
Until then `X-Forwarded-*` headers are ignored entirely, which is the safe default. No proxy
configuration is bundled; any of Caddy, nginx or Traefik works with a plain `reverse_proxy` /
`proxy_pass` to port 8080.

## Single sign-on (OpenID Connect)

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
