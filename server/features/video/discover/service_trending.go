package discover

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// TrendingParams carries the raw query strings of GET /video/trending; the
// service owns validation and defaulting.
type TrendingParams struct {
	Window string // day | week; "" defaults to day
	Type   string // all | movie | show; "" defaults to all
	Page   string // 1..500; "" defaults to 1
}

// DiscoverParams carries the raw query strings of GET /video/discover. Every
// filter is optional; "" or an empty slice means no constraint. The repeated
// ids arrive as the raw strings of `?genre=28&genre=35`.
type DiscoverParams struct {
	Type      string // all | movie | show; "" defaults to all
	Sort      string // popularity | rating | newest | votes; "" defaults to popularity
	Page      string // 1..500; "" defaults to 1
	VoteMin   string // TMDB score 0–10
	VoteMax   string
	VotesMin  string // minimum vote count
	Language  string // ISO 639-1 original language
	YearFrom  string
	YearTo    string
	Genres    []string // TMDB genre ids; a title must carry EVERY one
	Providers []string // TMDB watch-provider ids; a title on ANY of them, in the server's region
}

// TrendingEntry is one title of a trending or discover list: the search card's
// data — a foreign title addressed by its TMDB id, because the list is served
// live from TMDB and never stored (D-034) — plus the person page's library
// marks (D-027): the local id when the catalog holds the title, and THIS
// user's watched flags. The marks come from a local join, never a second
// provider call. The embedded struct flattens into the JSON object, so the
// wire shape is SearchResult's fields with three more.
type TrendingEntry struct {
	SearchResult
	// Local row id when the library holds this title (the card then links by
	// it, like a watchlist card), null when it does not.
	ID *int64 `json:"id"`
	// Watched is the watchlist page's meaning per kind — movie: any watch in
	// the log; show: COMPLETED (D-012). InHistory is the looser "seen any of
	// it": the same fact for a movie, any episode watch for a show.
	Watched   bool `json:"watched"`
	InHistory bool `json:"in_history"`
}

// TrendingPage is one page of either list: the entries plus TMDB's paging
// envelope, which is what the "Load more" button reads — there is more while
// page < total_pages. Entries is never nil.
type TrendingPage struct {
	Entries    []TrendingEntry `json:"entries"`
	Page       int             `json:"page"`
	TotalPages int             `json:"total_pages"`
}

// DiscoverOptions is what the Discover filter panel offers: TMDB's genre
// lists per kind, its language list, and the streaming services it knows in
// the server's watch region. Live from TMDB like the lists themselves, cached
// in the provider for the process (D-034).
type DiscoverOptions struct {
	Genres    DiscoverGenres   `json:"genres"`
	Languages []LanguageOption `json:"languages"`
	Providers []ProviderOption `json:"providers"`
	Region    string           `json:"region"` // the configured watch region the providers are for
}

type DiscoverGenres struct {
	Movie []GenreOption `json:"movie"`
	Show  []GenreOption `json:"show"`
}

type GenreOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type LanguageOption struct {
	Code string `json:"code"` // ISO 639-1
	Name string `json:"name"` // English name
}

type ProviderOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Relative URL of the locally proxied logo (the cache, never data/assets),
	// or null when TMDB has none.
	Logo *string `json:"logo"`
}

// Constants of the Discover defaults worth naming.
const (
	// discoverRatingMinVotes is the vote floor applied to the "highest rated"
	// order when the caller set none — TMDB's own site does the same, because
	// without it one-vote 10.0s lead the list.
	discoverRatingMinVotes = 200
	discoverMinYear        = 1870 // the first films
	discoverMaxYear        = 2100
	discoverMaxIDs         = 20 // genres or providers per request
)

var languageCodeRe = regexp.MustCompile(`^[a-z]{2}$`)

// Trending lists what is trending on TMDB today or this week, live on every
// call (D-034): validate, fetch one page, mark against the library, project.
// "all" reads TMDB's own cross-type list so the ranking is TMDB's — unlike
// Search's "all", which has no cross-type list to read and merges two by
// popularity — and drops the people TMDB ranks alongside, so a page can come
// up a few short of twenty. A single kind reads that kind's list in TMDB's
// order. TMDB offers no filters on this list, so neither does this route. The
// fetch comes before the library reads so an outage is reported as the outage
// it is. Nothing is written, ever.
func (s *Service) Trending(ctx context.Context, userID int64, p TrendingParams) (TrendingPage, error) {
	window := p.Window
	if window == "" {
		window = "day"
	}
	if window != "day" && window != "week" {
		return TrendingPage{}, fmt.Errorf("%w: window must be day or week", httpx.ErrValidation)
	}
	kind, err := listKind(p.Type)
	if err != nil {
		return TrendingPage{}, err
	}
	page, err := parsePage(p.Page)
	if err != nil {
		return TrendingPage{}, err
	}

	// The service speaks the app's vocabulary (show), the provider TMDB's (tv).
	mediaType := kind
	if kind == "show" {
		mediaType = "tv"
	}
	fetched, err := s.TMDB.Trending(ctx, mediaType, window, page)
	if err != nil {
		return TrendingPage{}, err
	}

	results := make([]SearchResult, 0, len(fetched.Results)) // never nil: an empty list is [], not null
	for _, r := range fetched.Results {
		// A typed list's rows name their kind too, but the kind asked for is
		// the fact; the discriminator only decides anything under "all".
		rowKind := kind
		if kind == "all" {
			switch r.MediaType {
			case "movie":
				rowKind = "movie"
			case "tv":
				rowKind = "show"
			default:
				continue // a person, or something TMDB adds later
			}
		}
		if r.ID == 0 {
			continue
		}
		results = append(results, listRowResult(rowKind, r))
	}
	return s.markPage(ctx, userID, results, page, fetched.TotalPages)
}

// Discover lists TMDB's catalog filtered and sorted the way the caller asked,
// live on every call (D-034); with no filter and the default sort it is
// TMDB's popular list. TMDB publishes movies and shows as two separate
// discover lists, so "all" is both kinds' page merged by the sort's own key
// and NOT capped — a cap would drop titles no later page brings back — while
// a single kind keeps TMDB's native order.
func (s *Service) Discover(ctx context.Context, userID int64, p DiscoverParams) (TrendingPage, error) {
	kind, err := listKind(p.Type)
	if err != nil {
		return TrendingPage{}, err
	}
	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return TrendingPage{}, err
	}
	q, err := s.discoverQuery(p, clk.Today)
	if err != nil {
		return TrendingPage{}, err
	}

	var rows []scoredResult
	totalPages := 0
	fetch := func(mediaType, rowKind string) error {
		fetched, err := s.TMDB.Discover(ctx, mediaType, q)
		if err != nil {
			return err
		}
		totalPages = max(totalPages, fetched.TotalPages)
		for _, r := range fetched.Results {
			if r.ID == 0 {
				continue
			}
			rows = append(rows, scoredResult{res: listRowResult(rowKind, r), key: discoverKey(q.Sort, r)})
		}
		return nil
	}
	if kind == "all" || kind == "movie" {
		if err := fetch("movie", "movie"); err != nil {
			return TrendingPage{}, err
		}
	}
	if kind == "all" || kind == "show" {
		if err := fetch("tv", "show"); err != nil {
			return TrendingPage{}, err
		}
	}
	if kind == "all" {
		rows = mergeByKey(rows)
	}

	results := make([]SearchResult, 0, len(rows)) // never nil
	for _, r := range rows {
		results = append(results, r.res)
	}
	return s.markPage(ctx, userID, results, q.Page, totalPages)
}

// discoverQuery validates the raw filter strings into the provider's query.
// Every refusal is a 422 naming the parameter. today is the viewer's day
// (D-038), the cap on the "newest" order.
func (s *Service) discoverQuery(p DiscoverParams, today string) (provider.DiscoverQuery, error) {
	var q provider.DiscoverQuery
	switch p.Sort {
	case "", "popularity":
		q.Sort = provider.DiscoverByPopularity
	case "rating":
		q.Sort = provider.DiscoverByRating
	case "newest":
		q.Sort = provider.DiscoverByNewest
	case "votes":
		q.Sort = provider.DiscoverByVotes
	default:
		return q, fmt.Errorf("%w: sort must be popularity, rating, newest or votes", httpx.ErrValidation)
	}
	page, err := parsePage(p.Page)
	if err != nil {
		return q, err
	}
	q.Page = page

	if q.VoteMin, err = parseScore("vote_min", p.VoteMin); err != nil {
		return q, err
	}
	if q.VoteMax, err = parseScore("vote_max", p.VoteMax); err != nil {
		return q, err
	}
	if q.VoteMin != nil && q.VoteMax != nil && *q.VoteMin > *q.VoteMax {
		return q, fmt.Errorf("%w: vote_min must not exceed vote_max", httpx.ErrValidation)
	}
	if p.VotesMin != "" {
		n, err := strconv.Atoi(p.VotesMin)
		if err != nil || n < 0 {
			return q, fmt.Errorf("%w: votes_min must be a non-negative integer", httpx.ErrValidation)
		}
		q.VotesMin = &n
	}
	if p.Language != "" {
		if !languageCodeRe.MatchString(p.Language) {
			return q, fmt.Errorf("%w: language must be a two-letter ISO 639-1 code", httpx.ErrValidation)
		}
		q.Language = p.Language
	}
	if q.YearFrom, err = parseYear("year_from", p.YearFrom); err != nil {
		return q, err
	}
	if q.YearTo, err = parseYear("year_to", p.YearTo); err != nil {
		return q, err
	}
	if q.YearFrom != nil && q.YearTo != nil && *q.YearFrom > *q.YearTo {
		return q, fmt.Errorf("%w: year_from must not exceed year_to", httpx.ErrValidation)
	}
	if q.Genres, err = parseIDList("genre", p.Genres); err != nil {
		return q, err
	}
	if q.Providers, err = parseIDList("provider", p.Providers); err != nil {
		return q, err
	}

	// Two defaults that keep the orders honest. The vote floor on "highest
	// rated" is TMDB's own site's habit; the date cap on "newest" keeps
	// announced titles from leading a list that reads as "what is new".
	if q.Sort == provider.DiscoverByRating && q.VotesMin == nil {
		n := discoverRatingMinVotes
		q.VotesMin = &n
	}
	if q.Sort == provider.DiscoverByNewest {
		q.DateTo = today
	}
	if len(q.Providers) > 0 {
		// The one call that carries the configured region to TMDB (D-034).
		q.WatchRegion = s.WatchRegion
	}
	return q, nil
}

// DiscoverOptions returns the filter panel's option lists, live from TMDB
// through the provider's process-lifetime cache. Languages are sorted by
// name with TMDB's "No Language" placeholder dropped; the providers are the
// movie and show lists of the configured region merged by id, in TMDB's
// display order.
func (s *Service) DiscoverOptions(ctx context.Context) (DiscoverOptions, error) {
	out := DiscoverOptions{Region: s.WatchRegion}

	movieGenres, err := s.TMDB.Genres(ctx, "movie")
	if err != nil {
		return DiscoverOptions{}, err
	}
	showGenres, err := s.TMDB.Genres(ctx, "tv")
	if err != nil {
		return DiscoverOptions{}, err
	}
	out.Genres.Movie = genreOptions(movieGenres)
	out.Genres.Show = genreOptions(showGenres)

	languages, err := s.TMDB.Languages(ctx)
	if err != nil {
		return DiscoverOptions{}, err
	}
	out.Languages = make([]LanguageOption, 0, len(languages))
	for _, l := range languages {
		if l.Code == "" || l.Code == "xx" || l.EnglishName == "" {
			continue
		}
		out.Languages = append(out.Languages, LanguageOption{Code: l.Code, Name: l.EnglishName})
	}
	sort.Slice(out.Languages, func(i, j int) bool {
		if out.Languages[i].Name != out.Languages[j].Name {
			return out.Languages[i].Name < out.Languages[j].Name
		}
		return out.Languages[i].Code < out.Languages[j].Code
	})

	movieProviders, err := s.TMDB.WatchProviders(ctx, "movie", s.WatchRegion)
	if err != nil {
		return DiscoverOptions{}, err
	}
	showProviders, err := s.TMDB.WatchProviders(ctx, "tv", s.WatchRegion)
	if err != nil {
		return DiscoverOptions{}, err
	}
	type ranked struct {
		opt      ProviderOption
		priority int
	}
	seen := map[int64]bool{}
	var providers []ranked
	for _, p := range slices.Concat(movieProviders, showProviders) {
		if p.ID == 0 || p.Name == "" || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		opt := ProviderOption{ID: p.ID, Name: p.Name}
		if p.LogoPath != "" {
			logo := common.PosterProxyURL(common.PosterSizeThumb, p.LogoPath)
			opt.Logo = &logo
		}
		providers = append(providers, ranked{opt: opt, priority: p.DisplayPriority})
	}
	sort.SliceStable(providers, func(i, j int) bool {
		if providers[i].priority != providers[j].priority {
			return providers[i].priority < providers[j].priority
		}
		return providers[i].opt.Name < providers[j].opt.Name
	})
	out.Providers = make([]ProviderOption, 0, len(providers))
	for _, p := range providers {
		out.Providers = append(out.Providers, p.opt)
	}
	return out, nil
}

func genreOptions(genres []provider.TMDBGenre) []GenreOption {
	out := make([]GenreOption, 0, len(genres))
	for _, g := range genres {
		if g.ID == 0 || g.Name == "" {
			continue
		}
		out = append(out, GenreOption{ID: g.ID, Name: g.Name})
	}
	return out
}

// listRowResult projects one trending/discover row as the given kind, reading
// the naming pair that kind uses.
func listRowResult(kind string, r provider.TMDBListResult) SearchResult {
	if kind == "movie" {
		return newSearchResult("movie", r.ID, r.Title, r.ReleaseDate, r.Overview, r.PosterPath, r.VoteAverage)
	}
	return newSearchResult("show", r.ID, r.Name, r.FirstAirDate, r.Overview, r.PosterPath, r.VoteAverage)
}

// discoverKey is the merge key for a combined discover page: the same number
// TMDB sorted each kind's list by, so the merge is the order the caller asked
// for. A date becomes a comparable day count; an unknown date sorts last.
func discoverKey(sortBy provider.DiscoverSort, r provider.TMDBListResult) float64 {
	switch sortBy {
	case provider.DiscoverByRating:
		return r.VoteAverage
	case provider.DiscoverByVotes:
		return float64(r.VoteCount)
	case provider.DiscoverByNewest:
		date := r.ReleaseDate
		if date == "" {
			date = r.FirstAirDate
		}
		if len(date) < 10 {
			return -1
		}
		y, _ := strconv.Atoi(date[:4])
		m, _ := strconv.Atoi(date[5:7])
		d, _ := strconv.Atoi(date[8:10])
		return float64(y*10000 + m*100 + d)
	default:
		return r.Popularity
	}
}

// listKind validates the shared type filter of the search, trending and
// discover lists, defaulting "" to all.
func listKind(raw string) (string, error) {
	if raw == "" {
		return "all", nil
	}
	if raw != "all" && raw != "movie" && raw != "show" {
		return "", fmt.Errorf("%w: type must be all, movie or show", httpx.ErrValidation)
	}
	return raw, nil
}

// parsePage reads a 1-based page number; "" is the first page. TMDB serves
// no page past 500.
func parsePage(raw string) (int, error) {
	if raw == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 500 {
		return 0, fmt.Errorf("%w: page must be an integer between 1 and 500", httpx.ErrValidation)
	}
	return n, nil
}

func parseScore(name, raw string) (*float64, error) {
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 || v > 10 {
		return nil, fmt.Errorf("%w: %s must be a number between 0 and 10", httpx.ErrValidation, name)
	}
	return &v, nil
}

func parseYear(name, raw string) (*int, error) {
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < discoverMinYear || n > discoverMaxYear {
		return nil, fmt.Errorf("%w: %s must be a year between %d and %d", httpx.ErrValidation, name, discoverMinYear, discoverMaxYear)
	}
	return &n, nil
}

// parseIDList reads a repeated id parameter (`?genre=28&genre=35` — never
// comma-joined, the match_user idiom): positive integers, no duplicates,
// at most discoverMaxIDs.
func parseIDList(name string, raw []string) ([]int64, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > discoverMaxIDs {
		return nil, fmt.Errorf("%w: at most %d %s values", httpx.ErrValidation, discoverMaxIDs, name)
	}
	out := make([]int64, 0, len(raw))
	for _, v := range raw {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("%w: %s must be a positive integer", httpx.ErrValidation, name)
		}
		if slices.Contains(out, id) {
			return nil, fmt.Errorf("%w: %s %d is repeated", httpx.ErrValidation, name, id)
		}
		out = append(out, id)
	}
	return out, nil
}

// scoredResult keeps a sort key alongside the DTO just long enough to merge
// two per-kind lists into one; single-kind lists pass through in TMDB order.
type scoredResult struct {
	res SearchResult
	key float64
}

// mergeByKey orders a combined movie+show list by its key, descending, with
// a full tie-break (title, then id) so two rows never swap places between
// renders. Search's "all" (by popularity, then capped) and Discover's "all"
// (by the chosen sort's key, uncapped) share this one rule.
func mergeByKey(rows []scoredResult) []scoredResult {
	slices.SortStableFunc(rows, func(a, b scoredResult) int {
		if a.key != b.key {
			if a.key > b.key {
				return -1
			}
			return 1
		}
		if c := strings.Compare(strings.ToLower(a.res.Title), strings.ToLower(b.res.Title)); c != 0 {
			return c
		}
		return int(a.res.TMDBID - b.res.TMDBID)
	})
	return rows
}

// markPage joins a foreign list against the catalog by tmdb_id (D-027's two
// compile-time whole-catalog reads) and wraps it in the paging envelope.
func (s *Service) markPage(ctx context.Context, userID int64, results []SearchResult, page, totalPages int) (TrendingPage, error) {
	movies, err := common.LibraryMovieFlags(ctx, s.DB, userID)
	if err != nil {
		return TrendingPage{}, err
	}
	shows, err := common.LibraryShowFlags(ctx, s.DB, userID)
	if err != nil {
		return TrendingPage{}, err
	}
	return TrendingPage{Entries: markedEntries(results, movies, shows), Page: page, TotalPages: totalPages}, nil
}

// markedEntries is the pure half of markPage, testable without a database.
func markedEntries(results []SearchResult, movies, shows map[int64]common.LibraryFlag) []TrendingEntry {
	out := make([]TrendingEntry, 0, len(results))
	for _, r := range results {
		e := TrendingEntry{SearchResult: r}
		flags := movies
		if r.MediaType == "show" {
			flags = shows
		}
		if f, ok := flags[r.TMDBID]; ok {
			id := f.ID
			e.ID, e.Watched, e.InHistory = &id, f.Watched, f.InHistory
		}
		out = append(out, e)
	}
	return out
}
