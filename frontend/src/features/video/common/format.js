// Display formatters shared by the video feature's detail pages.
//
// en-GB is pinned deliberately: it fixes the "28 Aug 2026" day-first order
// regardless of the browser's locale, which the compact chips and fact rows
// are sized for.
//
// Two kinds of value, two rules (D-038). A plain day (air_date, birthday) has
// no zone: it is anchored at UTC midnight and formatted in UTC, so no zone —
// the device's or the user's — can move it. An instant (published_at,
// fetched_at) is rendered in the user's zone through ../../../lib/time.js.

import { dayOf, today, zonedFormat } from '../../../lib/time.js'

/** @type {Intl.DateTimeFormatOptions} */
const dayOptions = { year: 'numeric', month: 'short', day: 'numeric' }
const dayFmt = new Intl.DateTimeFormat('en-GB', { ...dayOptions, timeZone: 'UTC' })

/**
 * A plain YYYY-MM-DD day as "28 Aug 2026". Split instead of new Date(day),
 * and anchored at UTC midnight with a UTC formatter, so the day printed is
 * the day stored, in every zone.
 * @param {string} day YYYY-MM-DD
 */
export function formatDay(day) {
  return dayFmt.format(utcDay(day))
}

/** @param {string} day YYYY-MM-DD */
function utcDay(day) {
  const [y, m, d] = day.split('-').map(Number)
  return new Date(Date.UTC(y, m - 1, d))
}

/**
 * The calendar day an instant falls on in the user's zone, as YYYY-MM-DD —
 * the day the server measures "today" by too (D-038).
 * @param {Date | string} d a Date or an RFC3339 instant
 */
export function localDayOf(d) {
  return dayOf(d)
}

/** The user's current day, YYYY-MM-DD, in their zone. */
export function todayLocal() {
  return today()
}

/**
 * Whole years lived between a YYYY-MM-DD birthday and a YYYY-MM-DD deathday,
 * or the viewer's local today when there is none — the person page's "(age
 * 62)". Split-not-parse like formatDay, so a birthday never shifts a day. Null when either date is not a full date (TMDB sometimes
 * knows only a year).
 *
 * @param {string} birthday
 * @param {string | null} deathday
 */
export function ageAt(birthday, deathday) {
  const [by, bm, bd] = birthday.split('-').map(Number)
  let ey, em, ed
  if (deathday) {
    ;[ey, em, ed] = deathday.split('-').map(Number)
  } else {
    ;[ey, em, ed] = todayLocal().split('-').map(Number)
  }
  if ([by, bm, bd, ey, em, ed].some((n) => !Number.isInteger(n))) return null
  let age = ey - by
  if (em < bm || (em === bm && ed < bd)) age--
  return age
}

const weekdayFmt = new Intl.DateTimeFormat('en-GB', {
  weekday: 'short',
  day: 'numeric',
  month: 'short',
  timeZone: 'UTC',
})
const weekdayYearFmt = new Intl.DateTimeFormat('en-GB', {
  weekday: 'short',
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  timeZone: 'UTC',
})

/**
 * A calendar heading for a plain day: the weekday and date, with the year only
 * once the day leaves the current one — "Thu 18 Sept" this year, "Sat 2 Jan
 * 2027" next. Same split-not-parse rule as formatDay.
 * @param {string} day YYYY-MM-DD
 * @param {string} today YYYY-MM-DD, the user's day
 */
export function formatCalendarDay(day, today) {
  return (day.slice(0, 4) === today.slice(0, 4) ? weekdayFmt : weekdayYearFmt).format(utcDay(day))
}

/**
 * Whole days from today to day, both YYYY-MM-DD days. Computed on UTC
 * midnights so a DST change inside the span cannot produce 0.96 of a day.
 * @param {string} day
 * @param {string} today
 */
export function daysFromToday(day, today) {
  const utc = (/** @type {string} */ s) => {
    const [y, m, d] = s.split('-').map(Number)
    return Date.UTC(y, m - 1, d)
  }
  return Math.round((utc(day) - utc(today)) / 86_400_000)
}

/**
 * Video publish dates only ever show a day, so the year alone would be too
 * coarse and the timestamp too noisy for a card.
 *
 * @param {string} ts RFC3339 instant
 */
export function formatVideoDate(ts) {
  const d = new Date(ts)
  return Number.isNaN(d.valueOf()) ? '' : zonedFormat('en-GB', dayOptions).format(d)
}

// TMDB reports the original language as an ISO 639-1 code.
const languageNames = new Intl.DisplayNames(['en'], { type: 'language' })

/** @param {string} code ISO 639-1 */
export function languageName(code) {
  // of() throws on a malformed tag; the raw code is an honest fallback.
  try {
    return languageNames.of(code) ?? code
  } catch {
    return code
  }
}

// ── Stats page formatting (shared rules of the Stats sections) ──────────────

/**
 * A headline count: 1,234,567 → "1.2M", 12,345 → "12.3k", 9,999 → "9999".
 * @param {number} n
 */
export function compactNumber(n) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 10_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

/**
 * A signed one-decimal delta with a true minus sign: "+0.4", "−0.4", and
 * "±0.0" when it rounds to zero.
 * @param {number} v
 */
export function signedDelta(v) {
  const r = Math.round(v * 10) / 10
  if (r === 0) return '±0.0'
  return r > 0 ? `+${r.toFixed(1)}` : `−${Math.abs(r).toFixed(1)}`
}

/**
 * "1 play" / "3 plays".
 * @param {number} n @param {string} one @param {string} many
 */
export function plural(n, one, many) {
  return `${n} ${n === 1 ? one : many}`
}

/**
 * How long ago a day was, by calendar day: "today", "yesterday", "N days
 * ago", then weeks (under 60 days), months (under 730) and years, with
 * integer division — the Stats page's "watched … ago". Both are YYYY-MM-DD
 * days, the first already the day the instant fell on in the user's zone.
 * @param {string} day
 * @param {string} today
 */
export function watchedAgo(day, today) {
  const n = -daysFromToday(day, today)
  if (n <= 0) return 'today'
  if (n === 1) return 'yesterday'
  if (n < 14) return `${n} days ago`
  if (n < 60) return `${Math.floor(n / 7)} weeks ago`
  if (n < 730) return `${Math.floor(n / 30)} months ago`
  return `${Math.floor(n / 365)} years ago`
}

const monthYearFmt = new Intl.DateTimeFormat('en-GB', {
  month: 'short',
  year: 'numeric',
  timeZone: 'UTC',
})
const monthOnlyFmt = new Intl.DateTimeFormat('en-GB', { month: 'short', timeZone: 'UTC' })

/**
 * A YYYY-MM month key, already bucketed server-side in the user's zone, as
 * "Oct 2026" — or just "Oct" with `short`. Anchored at UTC like formatDay,
 * so no zone can move it to the neighbouring month.
 * @param {string} ym YYYY-MM
 * @param {boolean} [short]
 */
export function formatMonth(ym, short = false) {
  const [y, m] = ym.split('-').map(Number)
  return (short ? monthOnlyFmt : monthYearFmt).format(new Date(Date.UTC(y, m - 1, 1)))
}

/**
 * A plain day with its weekday, "Wed 16 Oct 2026" — the heatmap's selected
 * day. Same split-not-parse rule as formatDay.
 * @param {string} day YYYY-MM-DD
 */
export function formatWeekdayDay(day) {
  return weekdayYearFmt.format(utcDay(day))
}
