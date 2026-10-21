// The single /api/v1 client. This module is the ONLY place fetch() appears —
// features build their API calls on top of these helpers and never call
// fetch themselves.

import { clearSession } from '../../features/auth/session.svelte.js'

export class ApiError extends Error {
  /**
   * @param {string} code stable machine string from the backend error envelope
   * @param {string} message human-readable message
   * @param {number} status HTTP status
   */
  constructor(code, message, status) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }
}

/**
 * @typedef {Object} RequestOpts
 * @property {boolean} [clearSessionOn401] default true. A 401 normally means
 *   the session expired, so the client resets the session state and App.svelte
 *   swaps the shell for the login page. Login passes false: its 401 means
 *   wrong credentials and must render inline instead.
 */

/**
 * @param {string} method
 * @param {string} path path under /api/v1, starting with '/'
 * @param {unknown} [body]
 * @param {RequestOpts} [opts]
 */
async function request(method, path, body, opts = {}) {
  // Relative URL (no leading slash) so the app works behind a sub-path proxy,
  // matching Vite's base: './'.
  const res = await fetch(`api/v1${path}`, {
    method,
    credentials: 'include', // auth is an HTTP-only session cookie
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  if (res.status === 401 && opts.clearSessionOn401 !== false) {
    // The one central place that handles session expiry.
    clearSession()
    throw new ApiError('unauthorized', 'session expired', 401)
  }
  if (!res.ok) {
    const payload = await res.json().catch(() => null)
    // Backend envelope: { error: { code, message } }. Branch on code, never message.
    throw new ApiError(
      payload?.error?.code ?? 'unknown',
      payload?.error?.message ?? res.statusText,
      res.status,
    )
  }
  return res.status === 204 ? null : res.json()
}

export const api = {
  /** @param {string} path @param {RequestOpts} [opts] */
  get: (path, opts) => request('GET', path, undefined, opts),
  /** @param {string} path @param {unknown} [body] @param {RequestOpts} [opts] */
  post: (path, body, opts) => request('POST', path, body, opts),
  /** @param {string} path @param {unknown} [body] @param {RequestOpts} [opts] */
  put: (path, body, opts) => request('PUT', path, body, opts),
  /** @param {string} path @param {RequestOpts} [opts] */
  del: (path, opts) => request('DELETE', path, undefined, opts),
}
