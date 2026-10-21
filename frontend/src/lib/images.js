// The single asset-URL helper, plus the srcset builders every poster, backdrop
// and still goes through. The API returns relative asset paths at BOTH provider
// sizes (e.g. "assets/video/movies/12/poster.thumb.jpg" beside ".../poster.jpg");
// assetUrl turns one into a URL that works behind a sub-path proxy, and the
// srcset helpers offer the pair so the browser picks by device pixel ratio ×
// rendered width. A card never chooses a variant itself — it states its
// rendered width through `sizes` and lets the browser decide.

// Inline neutral placeholder for items that legitimately have no image
// (non-Steam games).
const PLACEHOLDER =
  'data:image/svg+xml,' +
  encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 2 3"><rect width="2" height="3" fill="#2a2a35"/></svg>`,
  )

/** @param {string | null | undefined} path */
export function assetUrl(path) {
  return path ? path.replace(/^\/+/, '') : PLACEHOLDER
}

/**
 * srcset for a poster pair: the w185 thumb and the w500 full. `undefined` when
 * either is missing, so the attribute is omitted and `src` alone applies.
 * @param {string | null | undefined} thumb
 * @param {string | null | undefined} full
 */
export function posterSrcset(thumb, full) {
  return thumb && full ? `${assetUrl(thumb)} 185w, ${assetUrl(full)} 500w` : undefined
}

/**
 * srcset for a backdrop or episode-still pair: the w300 thumb and the w1280
 * full (TMDB's backdrop and still ladders share these widths).
 * @param {string | null | undefined} thumb
 * @param {string | null | undefined} full
 */
export function backdropSrcset(thumb, full) {
  return thumb && full ? `${assetUrl(thumb)} 300w, ${assetUrl(full)} 1280w` : undefined
}

// `sizes` hints shared by the cards that render at the same width. A missing
// `sizes` makes the browser assume the full viewport width and over-fetch, so
// every img with a srcset carries one; single-use values live beside their img.

/** The library grids: `minmax(var(--poster-min), 1fr)` cells, 130px min on phones. */
export const GRID_POSTER_SIZES = '(max-width: 767px) 45vw, 260px'

/** The horizontal rails (Similar titles, the Stats top-rated rail): 108px, 132px wide screens. */
export const RAIL_POSTER_SIZES = '(min-width: 1100px) 132px, 108px'

/** A page-wide banner (the Stats page is capped at 64rem). */
export const BANNER_SIZES = '(max-width: 767px) 100vw, 64rem'

/** Two banners side by side above the tablet breakpoint. */
export const HALF_BANNER_SIZES = '(max-width: 767px) 100vw, 32rem'
