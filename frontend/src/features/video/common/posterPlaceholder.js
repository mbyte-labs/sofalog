// Placeholder artwork for video items with no poster on disk — shared by the
// Up Next and History cards. Feature-local on purpose: only the video feature
// uses it, so it does not belong in lib/.
//
// While no real posters exist (M0), this is what makes a grid read as distinct
// cards rather than a wall of identical grey rectangles.

/**
 * A stable hue derived from the name, so the same show always gets the same
 * tint across pages and reloads.
 * @param {string} name
 */
export function hueFor(name) {
  let hue = 0
  for (const ch of name) hue = (hue * 31 + (ch.codePointAt(0) ?? 0)) % 360
  return hue
}

/** @param {string} name */
export function initialsFor(name) {
  const words = name.replace(/^(the|a|an)\s+/i, '').split(/\s+/)
  return words
    .slice(0, 2)
    .map((w) => w[0]?.toUpperCase() ?? '')
    .join('')
}
