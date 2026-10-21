// Auth feature API calls, built on the shared /api/v1 client — this module
// never touches fetch. Login opts out of the client's central 401 handling:
// its own 401 means wrong credentials and renders inline, not a session
// expiry that should bounce the app to the login page.

import { api } from '../../lib/api/client.js'

/** @typedef {import('./session.svelte.js').SessionUser} SessionUser */

/**
 * The boot probe: setup needed, logged out, or logged in as whom.
 * @returns {Promise<{
 *   status: 'setup_required' | 'unauthenticated' | 'authenticated',
 *   user?: SessionUser,
 *   auth?: { local: boolean, oidc: boolean, oidc_name?: string },
 * }>}
 */
export function getSessionStatus() {
  return api.get('/auth/session')
}

/**
 * @param {string} email
 * @param {string} password
 * @returns {Promise<{ user: SessionUser }>}
 */
export function login(email, password) {
  return api.post('/auth/login', { email, password }, { clearSessionOn401: false })
}

/**
 * First-run setup: creates the first account, as admin, and logs it in.
 * @param {string} email
 * @param {string} password
 * @param {string} [displayName]
 * @returns {Promise<{ user: SessionUser }>}
 */
export function setup(email, password, displayName) {
  return api.post('/auth/setup', { email, password, display_name: displayName ?? '' })
}

/** @returns {Promise<null>} */
export function logout() {
  return api.post('/auth/logout')
}

/**
 * Admin only — creates a regular (non-admin) account.
 * @param {string} email the login identifier
 * @param {string} password
 * @param {string} [displayName] optional label
 * @returns {Promise<{ user: SessionUser }>}
 */
export function createUser(email, password, displayName) {
  return api.post('/auth/users', { email, password, display_name: displayName ?? '' })
}

/**
 * One row of the admin user list.
 * @typedef {Object} UserListEntry
 * @property {number} id
 * @property {string} email the login identifier, and what an OIDC sign-in matches on
 * @property {string} display_name optional label; empty until set
 * @property {boolean} is_admin
 * @property {string} created_at RFC3339 UTC
 * @property {boolean} linked whether the account already has an SSO identity
 */

/**
 * Admin only — every account on the instance, ordered by email.
 * @returns {Promise<{ users: UserListEntry[] }>}
 */
export function listUsers() {
  return api.get('/auth/users')
}

/**
 * Change a password. An admin resets someone else's without a current
 * password; changing your own always needs it. The server revokes the
 * target's other sessions and leaves this device signed in.
 *
 * A wrong current password comes back 422 `validation`, never 401, so this
 * call cannot bounce the user out of the app — no opt-out needed.
 *
 * @param {number} id
 * @param {{ currentPassword?: string, newPassword: string }} passwords
 * @returns {Promise<null>}
 */
export function setPassword(id, { currentPassword, newPassword }) {
  return api.put(`/auth/users/${id}/password`, {
    current_password: currentPassword ?? '',
    new_password: newPassword,
  })
}

/**
 * Admin only — deletes the account and everything that user tracked.
 * @param {number} id
 * @returns {Promise<null>}
 */
export function deleteUser(id) {
  return api.del(`/auth/users/${id}`)
}

/**
 * The backend builds validation messages by wrapping its ErrValidation
 * sentinel, so they reach the client as "invalid input: <the useful part>".
 * The prefix is server-side vocabulary, not something to show a household
 * user — strip it and keep the actionable half.
 *
 * @param {string} message
 */
export function readableMessage(message) {
  return message.replace(/^invalid input:\s*/, '')
}

/**
 * Change an account's email — the login identifier. An admin may change
 * anyone else's; changing your own requires the current password.
 *
 * A wrong current password comes back 422, never 401, so this cannot bounce
 * the user out of the app.
 *
 * @param {number} id
 * @param {string} email
 * @param {string} [currentPassword] required when changing your own
 * @returns {Promise<null>}
 */
export function setEmail(id, email, currentPassword) {
  return api.put(`/auth/users/${id}/email`, {
    email,
    current_password: currentPassword ?? '',
  })
}

/**
 * Set or clear an account's display name. Your own, or anyone's if admin.
 * @param {number} id
 * @param {string} displayName empty clears it
 * @returns {Promise<null>}
 */
export function setDisplayName(id, displayName) {
  return api.put(`/auth/users/${id}/display-name`, { display_name: displayName })
}

/**
 * Allow or forbid other users matching their watchlist against yours (D-028).
 * Self only — the server refuses an admin acting on another account.
 * @param {number} id
 * @param {boolean} on
 * @returns {Promise<null>}
 */
export function setMatchWatchlist(id, on) {
  return api.put(`/auth/users/${id}/match-watchlist`, { match_watchlist: on })
}

/**
 * Set this account's time zone (D-038), or clear it back to the server
 * default with null. Self only, like the matching consent.
 * @param {number} id
 * @param {string | null} timezone an IANA name, e.g. "Europe/Bucharest"
 * @returns {Promise<null>}
 */
export function setTimezone(id, timezone) {
  return api.put(`/auth/users/${id}/timezone`, { timezone })
}

/**
 * The label to render for an account. Falls back to the email's local part,
 * so a user who has not set a name still shows as something recognisable
 * rather than blank. One definition, used by the sidebar and the user list —
 * forms bind to the raw display_name instead, so an unset name shows an
 * empty input rather than a prefilled guess.
 *
 * @param {{ display_name?: string, email?: string } | null | undefined} user
 */
export function displayLabel(user) {
  if (!user) return ''
  return user.display_name || (user.email ?? '').split('@')[0]
}

/**
 * The single sign-on entry point. This is a full browser navigation, not a
 * fetch — the IdP redirect chain cannot be followed by XHR.
 *
 * Resolved against document.baseURI so it works under a sub-path deployment
 * and from a deep route alike; a bare relative string would resolve against
 * the current path instead.
 *
 * @param {string} [next] path to return to after signing in
 */
export function ssoLoginUrl(next) {
  const url = new URL('api/v1/auth/oidc/start', document.baseURI)
  if (next) url.searchParams.set('next', next)
  return url.toString()
}
