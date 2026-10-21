// Pure label helpers for the Stats sections: routes, part-of-day copy,
// verdict sentences. No state, no DOM — the sections stay markup.

/**
 * The details page of a stats title. Every stats title is tracked, so it is
 * always addressed by its local id — the watchlist card's link.
 * @param {import('../common/api.js').StatsTitle} t
 */
export function titleHref(t) {
  return `/video/${t.kind}-details?id=${t.id}`
}

/**
 * A title's name, or the spec's stand-in when a stored row has none.
 * @param {import('../common/api.js').StatsTitle} t
 */
export function titleName(t) {
  return t.title || (t.kind === 'movie' ? 'A movie' : 'A show')
}

export const WEEKDAYS_SHORT = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
export const WEEKDAYS_LONG = [
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
  'Sunday',
]

/**
 * The four parts of the day, in the order ties resolve (earlier wins).
 * @type {{ key: 'morning' | 'afternoon' | 'evening' | 'night', label: string,
 *   range: string, persona: string, span: string }[]}
 */
export const PARTS = [
  {
    key: 'morning',
    label: 'Morning',
    range: '05-12',
    persona: 'Early bird',
    span: 'between 05:00 and 12:00',
  },
  {
    key: 'afternoon',
    label: 'Afternoon',
    range: '12-17',
    persona: 'Afternoon viewer',
    span: 'between 12:00 and 17:00',
  },
  {
    key: 'evening',
    label: 'Evening',
    range: '17-22',
    persona: 'Prime-time viewer',
    span: 'between 17:00 and 22:00',
  },
  { key: 'night', label: 'Night', range: '22-05', persona: 'Night owl', span: 'after 22:00' },
]

/**
 * A decade's axis label: "'90s" while the chart has few decades, the full
 * "1990" once there are ten or more.
 * @param {number} decade @param {boolean} short
 */
export function decadeLabel(decade, short) {
  return short ? `'${String(decade).slice(2)}s` : String(decade)
}

/** @param {'harsher' | 'kinder' | 'steady'} v */
export function trendVerdict(v) {
  if (v === 'harsher') return 'Rating more harshly lately'
  if (v === 'kinder') return 'Rating more generously lately'
  return 'Rating consistently'
}

/** @param {'harsher' | 'kinder' | 'in_sync'} v */
export function crowdVerdict(v) {
  if (v === 'harsher') return 'Tougher critic than the crowd'
  if (v === 'kinder') return 'Easier to please than the crowd'
  return 'In tune with the crowd'
}

/**
 * A whole percent of a share, with "<1%" and ">99%" standing in for a 0% or
 * 100% that still has some minutes behind it.
 * @param {number} part @param {number} total
 */
export function splitPercent(part, total) {
  if (total <= 0) return '0%'
  const p = Math.round((part / total) * 100)
  if (p === 0 && part > 0) return '<1%'
  if (p === 100 && part < total) return '>99%'
  return `${p}%`
}

/** @param {number} share 0..1 */
export function percent(share) {
  return `${Math.round(share * 100)}%`
}
