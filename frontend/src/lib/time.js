// The user's time zone (D-038), shared by every feature that shows an instant
// or asks what day it is.
//
// The server stores every instant in UTC and measures each user's "today" in
// one zone: the zone they picked on the Account page, else the server's
// DEFAULT_TIMEZONE. The session probe hands that resolved name over as
// `effective_timezone`, and everything here renders in it — never in the
// device's zone, which would let the season page and the server disagree
// about what has aired. Before sign-in there is no user, and the browser's own
// zone (timeZone: undefined) is the honest fallback.

import { session } from '../features/auth/session.svelte.js'

/** @returns {string | undefined} the IANA zone in force, undefined before sign-in */
export function userZone() {
  return session.user?.effective_timezone
}

/** @type {Map<string, Intl.DateTimeFormat>} */
const formats = new Map()

/**
 * An Intl.DateTimeFormat in the user's zone, cached per (locale, zone,
 * options) — so a zone change on the Account page takes effect on the next
 * render without a reload, and repeated renders build nothing.
 *
 * @param {string | undefined} locale undefined = the browser's locale
 * @param {Intl.DateTimeFormatOptions} options
 */
export function zonedFormat(locale, options) {
  const zone = userZone()
  const key = `${locale ?? ''}|${zone ?? ''}|${JSON.stringify(options)}`
  let f = formats.get(key)
  if (!f) {
    f = new Intl.DateTimeFormat(locale, { ...options, timeZone: zone })
    formats.set(key, f)
  }
  return f
}

/**
 * An RFC3339 instant as text in the user's zone; '' when it does not parse,
 * so a caller picks its own fallback.
 *
 * @param {string} ts
 * @param {Intl.DateTimeFormatOptions} [options] default: date and time
 * @param {string} [locale] default: the browser's
 */
export function formatInstant(ts, options = { dateStyle: 'medium', timeStyle: 'short' }, locale) {
  const d = new Date(ts)
  return Number.isNaN(d.getTime()) ? '' : zonedFormat(locale, options).format(d)
}

/**
 * The wall-clock fields of an instant in the user's zone. hourCycle h23, not
 * hour12: false — the latter renders midnight as "24" in some engines.
 *
 * @param {Date} d
 */
export function zonedParts(d) {
  const parts = zonedFormat('en-US', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(d)
  /** @param {string} type */
  const get = (type) => Number(parts.find((p) => p.type === type)?.value)
  return {
    year: get('year'),
    month: get('month'),
    day: get('day'),
    hour: get('hour'),
    minute: get('minute'),
    second: get('second'),
  }
}

/** @param {number} n */
const pad = (n) => String(n).padStart(2, '0')

/**
 * The calendar day an instant falls on in the user's zone, as YYYY-MM-DD —
 * the form every stored air/release date takes, so the two compare as plain
 * strings. Never toISOString, which reads the instant in UTC.
 *
 * @param {Date | string} at
 */
export function dayOf(at) {
  const p = zonedParts(typeof at === 'string' ? new Date(at) : at)
  return `${p.year}-${pad(p.month)}-${pad(p.day)}`
}

/** The user's current day, YYYY-MM-DD — the same day the server calls today. */
export function today() {
  return dayOf(new Date())
}

/**
 * The instant a wall-clock time names in the user's zone — what a date field
 * plus a time field mean. The zone's offset depends on the instant itself
 * (DST), so the offsets in force a day either side and at the naive guess
 * each propose an instant, and the ones that read back as the same wall time
 * are the answers. A time a fall-back repeats takes its FIRST occurrence; a
 * time a spring-forward skips has none and lands just after the jump, as the
 * platform's own Date does.
 *
 * @param {string} day YYYY-MM-DD
 * @param {number} hour
 * @param {number} minute
 * @returns {Date | null} null for a malformed day
 */
export function wallTimeToInstant(day, hour, minute) {
  const [y, m, d] = day.split('-').map(Number)
  if ([y, m, d, hour, minute].some((n) => !Number.isFinite(n))) return null
  const wall = Date.UTC(y, m - 1, d, hour, minute)
  if (Number.isNaN(wall)) return null
  /** @param {number} t a whole-second instant */
  const offsetAt = (t) => {
    const p = zonedParts(new Date(t))
    return Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second) - t
  }
  const dayMs = 86_400_000
  const candidates = [wall - dayMs, wall, wall + dayMs].map((probe) => wall - offsetAt(probe))
  const valid = candidates.filter((t) => t + offsetAt(t) === wall)
  return new Date(valid.length > 0 ? Math.min(...valid) : Math.max(...candidates))
}
