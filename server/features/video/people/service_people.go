package people

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// The person page (D-027): what a cast card links to. It is SPEC §5's call
// site 5 — a details view — in its purest form: a person has no catalog table,
// no snapshot, no per-user state and no tracking action, so EVERY view is
// served live from TMDB and discarded, and TMDB being down is a 503 on every
// view rather than only an untracked one. The one local read is the library
// join below, which marks the filmography's cards; it never calls anything.

// PersonDetailsParams carries the raw query string of GET /video/people/details;
// the service owns validation. tmdb_id only — a person never has a local id.
type PersonDetailsParams struct {
	TMDBID string
}

// PersonCredit is one filmography card: a title the person acted in, addressed
// by tmdb_id like a search result or a similar-title card, plus the two library
// fields — id when the catalog holds it (a local join by tmdb_id) and whether
// this user has watched it, with the watchlist page's meaning per kind (movie:
// any watch in the log; show: completed). Nullable fields are pointers without
// omitempty — the SPA distinguishes null from absent.
type PersonCredit struct {
	MediaType string  `json:"media_type"` // movie | show
	TMDBID    int64   `json:"tmdb_id"`
	Title     string  `json:"title"`
	Year      *string `json:"year"` // "1999", or null when TMDB has no date
	// Character is the part, or the parts joined " / " when TMDB credited the
	// person more than once in the title — one card per title, never one per
	// part. Null when TMDB names no part.
	Character *string `json:"character"`
	// TMDB community score (0–10); null when TMDB reports 0 ("unrated").
	Rating *float64 `json:"rating"`
	// Relative URLs of the locally proxied poster variants (w185 / w500), or
	// null when TMDB has none. Never a provider URL; never data/assets.
	Poster     *string `json:"poster"`
	PosterFull *string `json:"poster_full"`
	// Episodes is a SHOW figure: how many episodes the person appears in — the
	// LARGEST count among their parts, not the sum, since two parts can share
	// episodes and a sum would exceed the show's length. Null for a movie.
	Episodes *int `json:"episodes"`
	// ID is the local catalog id when the library holds this title, so the
	// card can link by id like a watchlist card; null when it does not, and the
	// card links by tmdb_id instead. Being non-null is what "in your library"
	// means.
	ID      *int64 `json:"id"`
	Watched bool   `json:"watched"`
	// InHistory is whether any watch of the title is in this user's log — for
	// a show, ANY episode, finished or not. Contrast Watched, which for a show
	// means completed: a half-watched show is in the history and not watched.
	// This is what the page's "Seen with" grid filters on. Always false when
	// ID is null.
	InHistory bool `json:"in_history"`
}

// PersonDetails is the person page's DTO, built from TMDB's live answer and
// the library join — never a raw provider response. No id (a person never has
// a row), no fetched_at (the answer is always now). Nullable fields are
// pointers without omitempty; slices are never null.
type PersonDetails struct {
	TMDBID             int64    `json:"tmdb_id"`
	Name               string   `json:"name"`
	Biography          *string  `json:"biography"`
	KnownForDepartment *string  `json:"known_for_department"` // "Acting", "Directing", …
	Birthday           *string  `json:"birthday"`             // YYYY-MM-DD
	Deathday           *string  `json:"deathday"`             // YYYY-MM-DD
	PlaceOfBirth       *string  `json:"place_of_birth"`
	AlsoKnownAs        []string `json:"also_known_as"`
	Homepage           *string  `json:"homepage"` // outbound link, legitimately external
	IMDBID             *string  `json:"imdb_id"`  // "nm0000093"
	// Relative URLs of the locally proxied portrait variants (w185 / h632 —
	// TMDB's profile ladder, not the poster one), or null when TMDB has none.
	Photo     *string        `json:"photo"`
	PhotoFull *string        `json:"photo_full"`
	Credits   []PersonCredit `json:"credits"` // acting credits, one per title
}

// PersonDetails returns one person, live from TMDB on every call (D-027):
// validate, fetch, mark against the library, project. The fetch comes before
// the library reads so an outage is reported as the outage it is, and a bogus
// id as the 404 TMDB answers with. Nothing is written, ever.
func (s *Service) PersonDetails(ctx context.Context, userID int64, p PersonDetailsParams) (PersonDetails, error) {
	tmdbID, err := strconv.ParseInt(p.TMDBID, 10, 64)
	if err != nil || tmdbID < 1 {
		return PersonDetails{}, fmt.Errorf("%w: tmdb_id must be a positive integer", httpx.ErrValidation)
	}

	person, err := s.TMDB.PersonDetails(ctx, tmdbID)
	if err != nil {
		return PersonDetails{}, err
	}

	movies, err := common.LibraryMovieFlags(ctx, s.DB, userID)
	if err != nil {
		return PersonDetails{}, err
	}
	shows, err := common.LibraryShowFlags(ctx, s.DB, userID)
	if err != nil {
		return PersonDetails{}, err
	}
	return personDetailsFrom(person, movies, shows), nil
}

// personDetailsFrom projects TMDB's person into the DTO. Pure, so the mapping
// and the credit rules are testable without a database.
func personDetailsFrom(p provider.TMDBPersonDetails, movies, shows map[int64]common.LibraryFlag) PersonDetails {
	d := PersonDetails{
		TMDBID:             p.ID,
		Name:               p.Name,
		Biography:          common.NilIfEmpty(p.Biography),
		KnownForDepartment: common.NilIfEmpty(p.KnownForDepartment),
		Birthday:           common.NilIfEmpty(p.Birthday),
		Deathday:           common.NilIfEmpty(p.Deathday),
		PlaceOfBirth:       common.NilIfEmpty(p.PlaceOfBirth),
		AlsoKnownAs:        make([]string, 0, len(p.AlsoKnownAs)),
		Homepage:           common.NilIfEmpty(p.Homepage),
		IMDBID:             common.NilIfEmpty(p.ExternalIDs.IMDBID),
	}
	for _, aka := range p.AlsoKnownAs {
		if strings.TrimSpace(aka) != "" {
			d.AlsoKnownAs = append(d.AlsoKnownAs, aka)
		}
	}
	if p.ProfilePath != "" {
		thumb := common.PosterProxyURL(common.PosterSizeThumb, p.ProfilePath)
		full := common.PosterProxyURL(common.ProfileSizeFull, p.ProfilePath)
		d.Photo, d.PhotoFull = &thumb, &full
	}
	d.Credits = personCredits(p.CombinedCredits.Cast, movies, shows)
	return d
}

// creditKey identifies one title in a person's credits. The pair, never the id
// alone: movie 550 and show 550 are different things (D-013), and TMDB's
// combined list holds both kinds.
type creditKey struct {
	kind string
	id   int64
}

// personCredits folds TMDB's combined cast credits — one entry per PART — into
// one card per title, marked against the library. Entries with no id, no
// title, or a media type other than movie/tv are dropped. Never nil.
//
// Order: undated first (announced work TMDB has no date for, the way its own
// page leads with it), then newest first, then title, kind and id — a full key,
// so two credits never swap places between renders. No cap: capping is
// presentation, and the SPA filters by kind.
func personCredits(cast []provider.TMDBPersonCastCredit, movies, shows map[int64]common.LibraryFlag) []PersonCredit {
	type acc struct {
		credit PersonCredit
		date   string
		parts  []string
	}
	byKey := map[creditKey]*acc{}
	order := make([]creditKey, 0, len(cast))

	for _, c := range cast {
		var kind string
		switch c.MediaType {
		case "movie":
			kind = "movie"
		case "tv":
			kind = "show"
		default:
			continue
		}
		title := c.Title
		if title == "" {
			title = c.Name
		}
		if c.ID == 0 || title == "" {
			continue
		}
		key := creditKey{kind: kind, id: c.ID}
		a, seen := byKey[key]
		if !seen {
			a = &acc{credit: PersonCredit{MediaType: kind, TMDBID: c.ID, Title: title}}
			a.date = c.ReleaseDate
			if a.date == "" {
				a.date = c.FirstAirDate
			}
			if c.VoteAverage > 0 {
				v := c.VoteAverage
				a.credit.Rating = &v
			}
			if c.PosterPath != "" {
				thumb := common.PosterProxyURL(common.PosterSizeThumb, c.PosterPath)
				full := common.PosterProxyURL(common.PosterSizeFull, c.PosterPath)
				a.credit.Poster, a.credit.PosterFull = &thumb, &full
			}
			byKey[key] = a
			order = append(order, key)
		}
		if part := strings.TrimSpace(c.Character); part != "" && !slices.Contains(a.parts, part) {
			a.parts = append(a.parts, part)
		}
		if kind == "show" && c.EpisodeCount > 0 && (a.credit.Episodes == nil || c.EpisodeCount > *a.credit.Episodes) {
			n := c.EpisodeCount
			a.credit.Episodes = &n
		}
	}

	out := make([]PersonCredit, 0, len(order))
	dates := make(map[creditKey]string, len(order))
	for _, key := range order {
		a := byKey[key]
		a.credit.Year = common.YearOf(a.date)
		a.credit.Character = common.NilIfEmpty(strings.Join(a.parts, " / "))
		flags := movies
		if key.kind == "show" {
			flags = shows
		}
		if f, ok := flags[key.id]; ok {
			id := f.ID
			a.credit.ID, a.credit.Watched, a.credit.InHistory = &id, f.Watched, f.InHistory
		}
		dates[key] = a.date
		out = append(out, a.credit)
	}

	slices.SortStableFunc(out, func(x, y PersonCredit) int {
		dx, dy := dates[creditKey{x.MediaType, x.TMDBID}], dates[creditKey{y.MediaType, y.TMDBID}]
		switch {
		case dx == "" && dy != "":
			return -1
		case dx != "" && dy == "":
			return 1
		case dx != dy:
			return strings.Compare(dy, dx) // YYYY-MM-DD compares lexically; newest first
		}
		if c := strings.Compare(strings.ToLower(x.Title), strings.ToLower(y.Title)); c != 0 {
			return c
		}
		if c := strings.Compare(x.MediaType, y.MediaType); c != 0 {
			return c
		}
		return int(x.TMDBID - y.TMDBID)
	})
	return out
}
