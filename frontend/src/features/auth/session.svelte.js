// The session state — the app's one shared rune module. App.svelte branches
// on it above the shell: the router and sidebar only mount when
// authenticated, so no page ever needs its own auth guard.
//
// This module holds state only and imports nothing: lib/api/client.js
// imports clearSession for its central 401 handling, so a fetch import here
// would be a cycle. The boot probe lives in App.svelte (via features/auth/api.js).

/**
 * @typedef {Object} SessionUser
 * @property {number} id
 * @property {string} email the login identifier
 * @property {string} display_name optional label; empty until set
 * @property {boolean} is_admin
 * @property {boolean} match_watchlist whether other users may pick this
 *   account in their watchlist's Matching filter (D-028); self-set only
 * @property {string | null} timezone the IANA zone this account chose
 *   (D-038); null follows the server default
 * @property {string} effective_timezone the zone in force — the choice, else
 *   the server's DEFAULT_TIMEZONE. Everything the SPA renders uses this one.
 */

/**
 * `status` drives the top-level branch in App.svelte:
 * - `unknown`         boot probe still in flight — render the splash
 * - `error`           boot probe failed (server unreachable) — offer retry
 * - `setup_required`  no account exists yet — render first-run setup
 * - `unauthenticated` render the login page
 * - `authenticated`   render the app shell; `user` is set
 *
 * `auth` reports which sign-in methods the deployment offers. They are
 * mutually exclusive: configuring an OIDC provider disables local
 * authentication entirely, so the login page renders either a password form
 * or a single sign-on button, never both.
 *
 * @type {{
 *   status: 'unknown' | 'error' | 'setup_required' | 'unauthenticated' | 'authenticated',
 *   user: SessionUser | null,
 *   auth: { local: boolean, oidc: boolean, oidc_name?: string },
 * }}
 */
export const session = $state({ status: 'unknown', user: null, auth: { local: true, oidc: false } })

/** @param {{ status: string, user?: SessionUser, auth?: { local: boolean, oidc: boolean, oidc_name?: string } }} payload the /auth/session probe result */
export function applyStatus(payload) {
  session.status = /** @type {typeof session.status} */ (payload.status)
  session.user = payload.user ?? null
  if (payload.auth) session.auth = payload.auth
}

/** @param {SessionUser} user */
export function setAuthenticated(user) {
  session.status = 'authenticated'
  session.user = user
}

// Called by the login/setup flows' logout and by the API client when any
// request comes back 401 — the session expired server-side.
export function clearSession() {
  session.status = 'unauthenticated'
  session.user = null
}

// The boot probe could not reach the server at all — distinct from
// unauthenticated, which would flash the login form at a user whose session
// may be perfectly valid.
export function markSessionError() {
  session.status = 'error'
  session.user = null
}
