// Value → color ramp for the user's own ratings: red (1–3), yellow (4–5),
// muted blue (6–7), accent blue (8–9), green (10). The mid-blue is
// deliberately desaturated so 8–9 still reads as a step up to the accent.
// Shared by the RatingBar and the season page's episode-rating average — the
// thresholds are >=, so a fractional average lands in the right band.

/** @param {number} v */
export function ratingColor(v) {
  if (v >= 10) return '#3fb760'
  if (v >= 8) return 'var(--accent)'
  if (v >= 6) return '#6c8cd5'
  if (v >= 4) return '#eab308'
  return '#e8435a'
}
