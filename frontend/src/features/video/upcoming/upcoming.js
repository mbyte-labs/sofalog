// The upcoming grid's presentation fold, kept out of the page so the card
// component can import the model it renders — a typedef inside a .svelte
// script is not importable. Feature-local like upNext.js.
//
// This is presentation, not derivation: the server orders the list by date,
// then title, then show id, season and episode, so one pass over adjacent
// entries is enough and nothing is re-sorted here.

import { daysFromToday, formatCalendarDay } from '../common/format.js'

/**
 * One card of the grid — the app's poster card, with the kind and the markers
 * stacked in the poster's top-left corner and the date in its bottom-right.
 *
 * @typedef {Object} UpcomingCardModel
 * @property {string} key list key, unique across the grid
 * @property {string} date YYYY-MM-DD
 * @property {string} dateLabel "Today", "Tomorrow", or the weekday and date
 * @property {boolean} soon whether the date is today or tomorrow — the badge
 *   is highlighted for those two
 * @property {string | null} href where the card leads, or null for a card with
 *   no page — a lone episode whose season row is gone
 * @property {string | null} poster relative asset path
 * @property {string | null} poster_full the same poster at the full size
 * @property {string} artKey the name the placeholder's hue and initials derive from
 * @property {string} title the show name or the movie title
 * @property {string} sub the second line: "S2xE5 · Title", "S2 · E1–E10", "Release"
 * @property {string} chip the kind, as visible text: "Episode", "10 episodes", "Movie"
 * @property {('season_premiere' | 'mid_season_finale' | 'season_finale')[]} markers
 */

/** @typedef {import('../common/api.js').UpcomingEpisodeEntry} EpisodeEntry */
/** @typedef {import('../common/api.js').UpcomingMovieEntry} MovieEntry */

/**
 * Fold the server-ordered list into cards, collapsing a run of consecutive
 * episodes of the same show and season on the same day into one card — a
 * whole season dropped on one day reads as "S2 · E1–E10", not ten cards. The
 * grid stays in the server's date order; each card carries its own date.
 *
 * @param {import('../common/api.js').UpcomingEntry[]} entries
 * @param {string} today the viewer's local day, YYYY-MM-DD — what "Today" means
 * @returns {UpcomingCardModel[]}
 */
export function buildCards(entries, today) {
  /** @type {(EpisodeEntry[] | MovieEntry)[]} */
  const groups = []
  for (const entry of entries) {
    if (entry.kind === 'movie') {
      groups.push(entry)
      continue
    }
    const prev = groups.at(-1)
    if (
      Array.isArray(prev) &&
      prev[0].date === entry.date &&
      prev[0].show.id === entry.show.id &&
      prev[0].episode.season === entry.episode.season
    ) {
      prev.push(entry)
    } else {
      groups.push([entry])
    }
  }
  return groups.map((g) => {
    const card = Array.isArray(g) ? episodeCard(g) : movieCard(g)
    const offset = daysFromToday(card.date, today)
    const relative = offset === 0 ? 'Today' : offset === 1 ? 'Tomorrow' : null
    card.dateLabel = relative ?? formatCalendarDay(card.date, today)
    card.soon = relative != null
    return card
  })
}

/**
 * A single episode links to its page by season id plus number (an episode has
 * no id-addressed page); a run links to the season page. Both hrefs use LOCAL
 * ids — an upcoming entry exists only for a show materialised here.
 *
 * @param {EpisodeEntry[]} run non-empty; one show, one season, in episode order
 * @returns {UpcomingCardModel}
 */
export function episodeCard(run) {
  const first = run[0]
  const last = /** @type {EpisodeEntry} */ (run.at(-1))
  const { season, season_id: seasonId } = first.episode
  if (run.length === 1) {
    const ep = first.episode
    return {
      key: `episode:${ep.id}`,
      date: first.date,
      dateLabel: '',
      soon: false,
      href:
        seasonId != null
          ? `/video/episode-details?season_id=${seasonId}&episode=${ep.number}`
          : null,
      poster: first.poster,
      poster_full: first.poster_full,
      artKey: first.show.name,
      title: first.show.name,
      sub: ep.title ? `S${season}xE${ep.number} · ${ep.title}` : `S${season}xE${ep.number}`,
      chip: 'Episode',
      markers: ep.marker ? [ep.marker] : [],
    }
  }
  // A run shows the premiere it opens with and the finale it closes on; a
  // mid-season finale inside it is a detail for the season page.
  /** @type {UpcomingCardModel['markers']} */
  const markers = []
  if (first.episode.marker === 'season_premiere') markers.push('season_premiere')
  if (last.episode.marker === 'season_finale') markers.push('season_finale')
  return {
    // The date is part of the key: the same season can drop a run on two
    // different days.
    key: `run:${first.show.id}:s${season}:${first.date}`,
    date: first.date,
    dateLabel: '',
    soon: false,
    href: seasonId != null ? `/video/season-details?id=${seasonId}` : null,
    poster: first.poster,
    poster_full: first.poster_full,
    artKey: first.show.name,
    title: first.show.name,
    // Min–max plus the count, so a gap in the numbering shows rather than
    // being papered over by the span.
    sub: `S${season} · E${first.episode.number}–E${last.episode.number}`,
    chip: `${run.length} episodes`,
    markers,
  }
}

/**
 * @param {MovieEntry} entry
 * @returns {UpcomingCardModel}
 */
export function movieCard(entry) {
  return {
    key: `movie:${entry.movie.id}`,
    date: entry.date,
    dateLabel: '',
    soon: false,
    href: `/video/movie-details?id=${entry.movie.id}`,
    poster: entry.poster,
    poster_full: entry.poster_full,
    artKey: entry.movie.title,
    title: entry.movie.title,
    sub: 'Release',
    chip: 'Movie',
    markers: [],
  }
}
