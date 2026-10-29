package discover

import (
	"context"
	"fmt"
	"strings"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// searchResultCap bounds a merged "all" search. Each TMDB call returns one
// page of up to 20 rows; merging two pages and keeping them all would push the
// least relevant matches of both types onto the screen.
const searchResultCap = 20

// SearchParams carries the raw query strings of GET /video/search; the
// service owns validation and defaulting.
type SearchParams struct {
	Q    string
	Type string
}

// SearchResult is one TMDB match, movie or show. Nothing here is library
// state: search results are ephemeral and never stored (SPEC §5). Nullable
// fields are pointers without omitempty — the SPA distinguishes null from
// absent.
type SearchResult struct {
	MediaType string  `json:"media_type"` // movie | show
	TMDBID    int64   `json:"tmdb_id"`
	Title     string  `json:"title"`
	Year      *string `json:"year"` // "1979", or null when TMDB has no date
	Overview  string  `json:"overview"`
	// TMDB community score (0–10); null when TMDB reports 0, which means
	// "unrated" rather than a genuine zero.
	Rating *float64 `json:"rating"`
	// Relative URL of the locally proxied thumb, or null when TMDB has no
	// poster. Never a provider URL — the browser only ever talks to us.
	Poster *string `json:"poster"`
	// Same image at the full detail-page size (w500); null exactly when
	// Poster is. Grids use Poster, the detail view uses this.
	PosterFull *string `json:"poster_full"`
}

// Search queries TMDB for movies and/or shows. Type defaults to "all", which
// merges both lists by popularity — the closest cross-type substitute for
// TMDB's per-list relevance order, which a merge necessarily loses. A
// single-type search keeps TMDB's native order.
func (s *Service) Search(ctx context.Context, p SearchParams) ([]SearchResult, error) {
	q := strings.TrimSpace(p.Q)
	if q == "" {
		return nil, fmt.Errorf("%w: q is required", httpx.ErrValidation)
	}
	kind, err := listKind(p.Type)
	if err != nil {
		return nil, err
	}

	var rows []scoredResult

	if kind == "all" || kind == "movie" {
		movies, err := s.TMDB.SearchMovies(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, m := range movies {
			rows = append(rows, scoredResult{res: movieSearchResult(m), key: m.Popularity})
		}
	}
	if kind == "all" || kind == "show" {
		shows, err := s.TMDB.SearchTV(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, t := range shows {
			rows = append(rows, scoredResult{res: showSearchResult(t), key: t.Popularity})
		}
	}

	if kind == "all" {
		// Merged by popularity and capped: keeping both pages whole would push
		// the least relevant matches of both types onto the screen.
		rows = mergeByKey(rows)
		if len(rows) > searchResultCap {
			rows = rows[:searchResultCap]
		}
	}

	results := make([]SearchResult, 0, len(rows)) // never nil: an empty search is [], not null
	for _, r := range rows {
		results = append(results, r.res)
	}
	return results, nil
}

func movieSearchResult(m provider.TMDBMovieResult) SearchResult {
	return newSearchResult("movie", m.ID, m.Title, m.ReleaseDate, m.Overview, m.PosterPath, m.VoteAverage)
}

func showSearchResult(t provider.TMDBTVResult) SearchResult {
	return newSearchResult("show", t.ID, t.Name, t.FirstAirDate, t.Overview, t.PosterPath, t.VoteAverage)
}

func newSearchResult(mediaType string, id int64, title, date, overview, posterPath string, vote float64) SearchResult {
	r := SearchResult{MediaType: mediaType, TMDBID: id, Title: title, Overview: overview}
	if len(date) >= 4 {
		year := date[:4]
		r.Year = &year
	}
	if vote > 0 {
		v := vote
		r.Rating = &v
	}
	if posterPath != "" {
		// posterPath arrives with its leading slash ("/abc.jpg"). The URLs are
		// relative — resolved against the SPA's <base>, like every asset path.
		u := "api/v1/video/posters/" + common.PosterSizeThumb + posterPath
		r.Poster = &u
		f := "api/v1/video/posters/" + common.PosterSizeFull + posterPath
		r.PosterFull = &f
	}
	return r
}
