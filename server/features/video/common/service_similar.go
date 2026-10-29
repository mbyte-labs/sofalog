package common

// "Similar titles" — TMDB's recommendations for an item, the details pages'
// "Similar movies" / "Similar shows" rail (D-024). Not a provider call site of
// its own: `recommendations` is appended to the /movie/{id} and /tv/{id} calls
// the snapshot already makes and parsed back out of the stored body, exactly as
// credits and images are — so a tracked item's rail renders with TMDB down.
//
// TMDB publishes recommendations per movie and per show only, so seasons and
// episodes have no rail and TVSeasonDetails does not append the section. A
// movie's list holds movies and a show's holds shows, which is why an entry
// carries no media type: the page it hangs off already knows the kind.
//
// Every entry names a title the catalog has no row for. Its poster therefore
// goes through the cache proxy (data/cache), never data/assets — it is not this
// item's artwork, and filing it under the item directory would put it in front
// of a reconciler that deletes whatever its desired set does not name.

// SimilarMax bounds the rail. TMDB returns one page of at most 20 and no second
// page is ever fetched, so today this is a no-op; it documents the ceiling.
const SimilarMax = 20

// snapshotRecommendation is one entry of the appended `recommendations`
// section. A /movie/{id} body carries title and release_date, a /tv/{id} body
// name and first_air_date; the reader takes whichever is set, so one projection
// serves both pages.
type snapshotRecommendation struct {
	ID           int64   `json:"id"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	PosterPath   string  `json:"poster_path"`
	VoteAverage  float64 `json:"vote_average"`
}

// SimilarTitle is one card of the "Similar" rail: a foreign title, addressed by
// its TMDB id because it has no local row. Nullable fields are pointers without
// omitempty — the SPA distinguishes null from absent.
type SimilarTitle struct {
	TMDBID int64   `json:"tmdb_id"`
	Title  string  `json:"title"`
	Year   *string `json:"year"` // "1999", or null when TMDB has no date
	// TMDB community score (0–10); null when TMDB reports 0 ("unrated").
	Rating *float64 `json:"rating"`
	// Relative URLs of the locally proxied poster variants (w185 / w500), or
	// null when TMDB has none. Never a provider URL.
	Poster     *string `json:"poster"`
	PosterFull *string `json:"poster_full"`
}

// SimilarTitles projects the snapshot's recommendations into rail entries, in
// TMDB's order — it is a relevance ranking, so nothing here re-sorts it — with
// entries missing an id or a title dropped, capped at SimilarMax. Never nil:
// a snapshot with no section and one listing nobody both render an empty rail.
func SimilarTitles(snap MediaSnapshot) []SimilarTitle {
	out := make([]SimilarTitle, 0, len(snap.Recommendations.Results))
	for _, r := range snap.Recommendations.Results {
		title := r.Title
		if title == "" {
			title = r.Name
		}
		if r.ID == 0 || title == "" {
			continue
		}
		date := r.ReleaseDate
		if date == "" {
			date = r.FirstAirDate
		}
		entry := SimilarTitle{TMDBID: r.ID, Title: title, Year: YearOf(date)}
		if r.VoteAverage > 0 {
			v := r.VoteAverage
			entry.Rating = &v
		}
		if r.PosterPath != "" {
			thumb := PosterProxyURL(PosterSizeThumb, r.PosterPath)
			full := PosterProxyURL(PosterSizeFull, r.PosterPath)
			entry.Poster, entry.PosterFull = &thumb, &full
		}
		out = append(out, entry)
		if len(out) == SimilarMax {
			break
		}
	}
	return out
}
