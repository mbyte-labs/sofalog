// The two strings an up-next entry turns into. Both the card and the page's
// chooser dialog need them — the card for its caption, the chooser for its
// heading and its link — and they must agree, or tapping "S1xE3" would open a
// dialog captioned something else.

/**
 * The episode's caption: unpadded and x-separated, "S10xE3", matching the
 * history card's.
 *
 * @param {import('../common/api.js').UpNextEntry} entry
 */
export function episodeCode(entry) {
  return `S${entry.episode.season}xE${entry.episode.number}`
}

/**
 * The episode details page, addressed by LOCAL catalog ids and never a tmdb id:
 * an up-next entry exists only for a show already materialised here, so
 * clicking through renders from the snapshot and touches no provider. An
 * episode has no id-addressed page, so it is addressed by its season plus its
 * number — the same form the history and season pages link with. Null when the
 * season row is gone, which is unreachable in practice; the chooser then simply
 * omits the link rather than dead-linking, and marking still works because
 * that only needs the episode's own id.
 *
 * @param {import('../common/api.js').UpNextEntry} entry
 * @returns {string | null}
 */
export function episodeHref(entry) {
  const { season_id: seasonID, number } = entry.episode
  return seasonID != null ? `/video/episode-details?season_id=${seasonID}&episode=${number}` : null
}
