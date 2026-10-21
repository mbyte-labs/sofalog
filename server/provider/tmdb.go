package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"mbyte.ro/sofalog/httpx"
)

const (
	tmdbBaseURL = "https://api.themoviedb.org/3"

	// TMDB publishes no hard rate limit any more but throttles abusive bursts.
	// 40 requests/second is comfortably under anything observed, and adding a
	// show fans out to hundreds of calls, so the pacing is not academic.
	tmdbInterval = 25 * time.Millisecond

	// tmdbTimeout bounds a single call. Provider slowness must never become
	// server slowness.
	tmdbTimeout = 10 * time.Second

	// tmdbMaxBody caps a decoded response. A season of a long-running show is
	// the biggest legitimate payload and is far under this.
	tmdbMaxBody = 8 << 20

	// tmdbMaxImage caps a downloaded image. A w500 poster is a few hundred KB;
	// anything near this limit is not a poster.
	tmdbMaxImage = 10 << 20
)

// TMDB is the client for themoviedb.org. Safe for concurrent use.
//
// The zero value is not usable; construct with NewTMDB.
type TMDB struct {
	// credential and auth are resolved once in NewTMDB, so the request path has
	// no credential decision left to make.
	credential string
	auth       authMode
	warnings   []string

	baseURL string
	http    Doer
	limit   *limiter

	mu       sync.Mutex
	imageCfg *ImageConfig
	// The Discover page's option lists, cached on the same terms as imageCfg
	// (D-034): genres and watch providers by kind (providers also by region).
	genres         map[string][]TMDBGenre
	languages      []TMDBLanguage
	watchProviders map[string][]TMDBWatchProviderOption
}

// authMode is how the resolved credential is presented to TMDB.
type authMode int

const (
	authNone authMode = iota
	// authQueryV3 sends the v3 API key as an api_key query parameter.
	authQueryV3
	// authBearerV4 sends the v4 read access token as an Authorization header.
	// TMDB documents this as valid for v3 endpoints too, which is all this app
	// calls — it never touches a /4/ endpoint.
	authBearerV4
)

// String names the mode for a log line. It describes the scheme, never the value.
func (a authMode) String() string {
	switch a {
	case authQueryV3:
		return "v3 api_key"
	case authBearerV4:
		return "v4 bearer"
	default:
		return "none"
	}
}

// TMDBCredentials is the pair TMDB issues, as shown on
// https://www.themoviedb.org/settings/api. Named fields rather than two string
// parameters: the whole point of keeping them apart is that they must not be
// interchangeable at the call site.
type TMDBCredentials struct {
	// APIKeyV3 is the "API Key" field — a short opaque string.
	APIKeyV3 string
	// APIKeyV4 is the "API Read Access Token" field — a JWT.
	APIKeyV4 string
}

// ImageConfig is TMDB's image base URL plus the size variants it offers. TMDB
// documents the base URL as changeable, so it is fetched once and cached rather
// than hardcoded anywhere in the codebase.
type ImageConfig struct {
	SecureBaseURL string   `json:"secure_base_url"`
	PosterSizes   []string `json:"poster_sizes"`
	StillSizes    []string `json:"still_sizes"`
	BackdropSizes []string `json:"backdrop_sizes"`
}

// tmdbConfigResponse is the /configuration envelope; only images is used.
type tmdbConfigResponse struct {
	Images ImageConfig `json:"images"`
}

// TMDBOption customises a client. Both options exist for tests — production
// passes none.
type TMDBOption func(*TMDB)

// WithHTTPDoer replaces the HTTP client. Tests inject a stub; nothing else should
// need this.
func WithHTTPDoer(d Doer) TMDBOption {
	return func(c *TMDB) { c.http = d }
}

// WithBaseURL points the client at a different host, e.g. an httptest server.
func WithBaseURL(u string) TMDBOption {
	return func(c *TMDB) { c.baseURL = strings.TrimRight(u, "/") }
}

// NewTMDB builds a client from whichever credentials are configured. Supplying
// neither is allowed and is not an error here: browsing an existing library must
// work without any key, and only the search and add paths fail (with a 503).
//
// A credential in the wrong field is discarded rather than used, and the reason is
// recorded in CredentialWarnings. Silently compensating for a swap would mean the
// variable did not mean what it says, which is the confusion this pair exists to
// remove.
func NewTMDB(creds TMDBCredentials, opts ...TMDBOption) *TMDB {
	c := &TMDB{
		baseURL: tmdbBaseURL,
		http:    &http.Client{Timeout: tmdbTimeout},
		limit:   newLimiter(tmdbInterval),
	}

	v3 := strings.TrimSpace(creds.APIKeyV3)
	v4 := strings.TrimSpace(creds.APIKeyV4)

	if v3 != "" && looksLikeV4(v3) {
		c.warnings = append(c.warnings,
			"TMDB_API_KEY_V3 holds a v4 read access token (a JWT); ignoring it — move the value to TMDB_API_KEY_V4")
		v3 = ""
	}
	if v4 != "" && !looksLikeV4(v4) {
		c.warnings = append(c.warnings,
			"TMDB_API_KEY_V4 does not hold a v4 read access token (expected a JWT); ignoring it — a v3 API key belongs in TMDB_API_KEY_V3")
		v4 = ""
	}

	// The v4 token wins when both survive: TMDB's recommended scheme, and it
	// keeps the secret in a header instead of the request URL.
	switch {
	case v4 != "":
		c.credential, c.auth = v4, authBearerV4
	case v3 != "":
		c.credential, c.auth = v3, authQueryV3
	}

	for _, opt := range opts {
		opt(c)
	}
	return c
}

// looksLikeV4 reports whether a value is a JWT, which is what TMDB issues as a v4
// read access token — "eyJ" is the base64 of a JWT header's opening {". Used to
// validate that each credential is in its own field, never to guess intent.
func looksLikeV4(value string) bool {
	return strings.HasPrefix(value, "eyJ")
}

// Configured reports whether a usable credential survived validation. Callers use
// it to fail fast with a clear message instead of issuing a request that can only
// be rejected.
func (c *TMDB) Configured() bool { return c.auth != authNone }

// AuthMode names the scheme in use ("v3 api_key", "v4 bearer" or "none") for a
// startup log line. It reveals the scheme, never the credential.
func (c *TMDB) AuthMode() string { return c.auth.String() }

// CredentialWarnings returns human-readable problems with the configured
// credentials — values ignored because they were in the wrong variable. Empty when
// the configuration is clean. main logs these at startup; nothing else reads them.
func (c *TMDB) CredentialWarnings() []string { return c.warnings }

// Configuration fetches TMDB's image configuration and caches it for the process
// lifetime. It doubles as the credential check at startup: a bad key surfaces
// here as an error rather than on a user's first search.
//
// Only a successful result is cached, so a failure at startup retries on first
// real use instead of poisoning the cache. Two concurrent first callers may both
// issue the request; that is preferred over holding the lock across a network
// call, and the request is an idempotent GET.
func (c *TMDB) Configuration(ctx context.Context) (ImageConfig, error) {
	c.mu.Lock()
	cached := c.imageCfg
	c.mu.Unlock()
	if cached != nil {
		return *cached, nil
	}

	var resp tmdbConfigResponse
	if err := c.get(ctx, "/configuration", nil, &resp); err != nil {
		return ImageConfig{}, err
	}
	if resp.Images.SecureBaseURL == "" {
		return ImageConfig{}, fmt.Errorf("%w: tmdb /configuration returned no image base url", httpx.ErrProviderUnavailable)
	}

	images := resp.Images
	c.mu.Lock()
	c.imageCfg = &images
	c.mu.Unlock()
	return images, nil
}

// TMDBMovieResult is one row of a /search/movie response. Fields the app never
// reads are not declared.
type TMDBMovieResult struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	ReleaseDate string  `json:"release_date"` // YYYY-MM-DD, or "" when unknown
	Overview    string  `json:"overview"`
	PosterPath  string  `json:"poster_path"` // "/abc.jpg", or "" when absent
	Popularity  float64 `json:"popularity"`
	VoteAverage float64 `json:"vote_average"`
}

// TMDBTVResult is one row of a /search/tv response. TMDB names the fields
// differently from movies (name/first_air_date), so it is a separate type
// rather than a lossy shared one.
type TMDBTVResult struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	FirstAirDate string  `json:"first_air_date"` // YYYY-MM-DD, or "" when unknown
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"` // "/abc.jpg", or "" when absent
	Popularity   float64 `json:"popularity"`
	VoteAverage  float64 `json:"vote_average"`
}

// SearchMovies queries /search/movie. First page only — search results are
// ephemeral and never written to the library, so paging through hundreds of
// matches serves nothing.
func (c *TMDB) SearchMovies(ctx context.Context, query string) ([]TMDBMovieResult, error) {
	var resp struct {
		Results []TMDBMovieResult `json:"results"`
	}
	if err := c.get(ctx, "/search/movie", searchQuery(query), &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// SearchTV queries /search/tv. First page only, same reasoning as SearchMovies.
func (c *TMDB) SearchTV(ctx context.Context, query string) ([]TMDBTVResult, error) {
	var resp struct {
		Results []TMDBTVResult `json:"results"`
	}
	if err := c.get(ctx, "/search/tv", searchQuery(query), &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

func searchQuery(query string) url.Values {
	return url.Values{
		"query":         {query},
		"include_adult": {"false"},
	}
}

// TMDBListResult is one row of a /trending or /discover response. The
// trending list is mixed — movies, shows and people side by side under "all" —
// so unlike the search rows it carries TMDB's media_type discriminator and
// BOTH naming pairs (title/release_date for a movie, name/first_air_date for
// a show); the caller reads the pair its media_type names. Fields the app
// never reads are not declared, and a person row is left to the caller to
// drop.
type TMDBListResult struct {
	ID               int64   `json:"id"`
	MediaType        string  `json:"media_type"` // movie | tv | person; "" on a typed discover list
	Title            string  `json:"title"`
	Name             string  `json:"name"`
	ReleaseDate      string  `json:"release_date"`   // YYYY-MM-DD, or "" when unknown
	FirstAirDate     string  `json:"first_air_date"` // YYYY-MM-DD, or "" when unknown
	Overview         string  `json:"overview"`
	PosterPath       string  `json:"poster_path"` // "/abc.jpg", or "" when absent
	OriginalLanguage string  `json:"original_language"`
	GenreIDs         []int64 `json:"genre_ids"`
	Popularity       float64 `json:"popularity"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int64   `json:"vote_count"`
}

// TMDBListPage is one page of a /trending or /discover list with the paging
// envelope the "Load more" button needs. TMDB never serves past page 500.
type TMDBListPage struct {
	Page       int              `json:"page"`
	TotalPages int              `json:"total_pages"`
	Results    []TMDBListResult `json:"results"`
}

// tmdbMaxPage is TMDB's own ceiling on list paging.
const tmdbMaxPage = 500

// Trending reads /trending/{mediaType}/{window} — TMDB's activity ranking for
// today or the past week, one page in TMDB's order (D-034). mediaType is
// TMDB's vocabulary (all | movie | tv) and window day | week; both are
// checked here before they are spliced into the path, so a caller's typo is a
// validation error and never a request. The list is discovery, served live
// and never written to the library.
func (c *TMDB) Trending(ctx context.Context, mediaType, window string, page int) (TMDBListPage, error) {
	if mediaType != "all" && mediaType != "movie" && mediaType != "tv" {
		return TMDBListPage{}, fmt.Errorf("%w: trending media type must be all, movie or tv", httpx.ErrValidation)
	}
	if window != "day" && window != "week" {
		return TMDBListPage{}, fmt.Errorf("%w: trending window must be day or week", httpx.ErrValidation)
	}
	if page < 1 || page > tmdbMaxPage {
		return TMDBListPage{}, fmt.Errorf("%w: page must be between 1 and %d", httpx.ErrValidation, tmdbMaxPage)
	}
	var resp TMDBListPage
	if err := c.get(ctx, "/trending/"+mediaType+"/"+window, url.Values{"page": {strconv.Itoa(page)}}, &resp); err != nil {
		return TMDBListPage{}, err
	}
	return resp, nil
}

// DiscoverSort names the orderings the Discover list offers, in the app's
// vocabulary; the provider translates each into TMDB's per-kind sort_by.
type DiscoverSort string

const (
	DiscoverByPopularity DiscoverSort = "popularity"
	DiscoverByRating     DiscoverSort = "rating"
	DiscoverByNewest     DiscoverSort = "newest"
	DiscoverByVotes      DiscoverSort = "votes"
)

// DiscoverQuery is one /discover request in kind-neutral terms. TMDB names the
// date parameters differently for movies (primary_release_date) and shows
// (first_air_date), and the "newest" sort follows suit, so the translation to
// TMDB's parameters lives here and the caller states what it means once. A
// nil pointer or empty slice is "no constraint".
type DiscoverQuery struct {
	Sort     DiscoverSort
	VoteMin  *float64 // vote_average.gte, 0–10
	VoteMax  *float64 // vote_average.lte
	VotesMin *int     // vote_count.gte
	Genres   []int64  // with_genres, comma-joined: every genre must match
	Language string   // with_original_language, ISO 639-1
	YearFrom *int     // the kind's date >= YYYY-01-01
	YearTo   *int     // the kind's date <= YYYY-12-31
	DateTo   string   // the kind's date <= YYYY-MM-DD; a finer cap than YearTo, the two combine
	// Providers filters to titles a listed streaming service carries in
	// WatchRegion (with_watch_providers, pipe-joined: any of them). TMDB needs
	// the region beside it, so it is sent exactly when Providers is set — the
	// one call that carries TMDB_WATCH_REGION to TMDB (D-034).
	Providers   []int64
	WatchRegion string
	Page        int
}

// Discover reads /discover/{mediaType} — TMDB's filtered, sorted catalog
// listing, of which the popular list is the unfiltered popularity order
// (D-034). mediaType is movie | tv; there is no cross-type discover list, so
// a combined page is the caller's merge.
func (c *TMDB) Discover(ctx context.Context, mediaType string, q DiscoverQuery) (TMDBListPage, error) {
	if mediaType != "movie" && mediaType != "tv" {
		return TMDBListPage{}, fmt.Errorf("%w: discover media type must be movie or tv", httpx.ErrValidation)
	}
	if q.Page < 1 || q.Page > tmdbMaxPage {
		return TMDBListPage{}, fmt.Errorf("%w: page must be between 1 and %d", httpx.ErrValidation, tmdbMaxPage)
	}
	dateKey := "primary_release_date"
	if mediaType == "tv" {
		dateKey = "first_air_date"
	}
	var sortBy string
	switch q.Sort {
	case DiscoverByPopularity, "":
		sortBy = "popularity.desc"
	case DiscoverByRating:
		sortBy = "vote_average.desc"
	case DiscoverByNewest:
		sortBy = dateKey + ".desc"
	case DiscoverByVotes:
		sortBy = "vote_count.desc"
	default:
		return TMDBListPage{}, fmt.Errorf("%w: unknown discover sort %q", httpx.ErrValidation, string(q.Sort))
	}

	query := url.Values{
		"sort_by":       {sortBy},
		"include_adult": {"false"},
		"page":          {strconv.Itoa(q.Page)},
	}
	if mediaType == "tv" {
		query.Set("include_null_first_air_dates", "false")
	}
	if q.VoteMin != nil {
		query.Set("vote_average.gte", strconv.FormatFloat(*q.VoteMin, 'f', -1, 64))
	}
	if q.VoteMax != nil {
		query.Set("vote_average.lte", strconv.FormatFloat(*q.VoteMax, 'f', -1, 64))
	}
	if q.VotesMin != nil {
		query.Set("vote_count.gte", strconv.Itoa(*q.VotesMin))
	}
	if len(q.Genres) > 0 {
		query.Set("with_genres", joinIDs(q.Genres, ","))
	}
	if q.Language != "" {
		query.Set("with_original_language", q.Language)
	}
	if q.YearFrom != nil {
		query.Set(dateKey+".gte", fmt.Sprintf("%04d-01-01", *q.YearFrom))
	}
	dateTo := q.DateTo
	if q.YearTo != nil {
		yearEnd := fmt.Sprintf("%04d-12-31", *q.YearTo)
		if dateTo == "" || yearEnd < dateTo {
			dateTo = yearEnd
		}
	}
	if dateTo != "" {
		query.Set(dateKey+".lte", dateTo)
	}
	if len(q.Providers) > 0 {
		if q.WatchRegion == "" {
			return TMDBListPage{}, fmt.Errorf("%w: a provider filter needs a watch region", httpx.ErrValidation)
		}
		query.Set("with_watch_providers", joinIDs(q.Providers, "|"))
		query.Set("watch_region", q.WatchRegion)
	}

	var resp TMDBListPage
	if err := c.get(ctx, "/discover/"+mediaType, query, &resp); err != nil {
		return TMDBListPage{}, err
	}
	return resp, nil
}

func joinIDs(ids []int64, sep string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, sep)
}

// TMDBLanguage is one row of /configuration/languages.
type TMDBLanguage struct {
	Code        string `json:"iso_639_1"`
	EnglishName string `json:"english_name"`
}

// TMDBWatchProviderOption is one row of /watch/providers/{movie|tv} — a
// streaming service available in the asked-for region, as a filter option.
type TMDBWatchProviderOption struct {
	ID              int64  `json:"provider_id"`
	Name            string `json:"provider_name"`
	LogoPath        string `json:"logo_path"`
	DisplayPriority int    `json:"display_priority"`
}

// Genres returns TMDB's genre list for movie or tv, cached for the process
// lifetime the way Configuration is: only a success is cached, and the lock is
// never held across the call. The lists are effectively static.
func (c *TMDB) Genres(ctx context.Context, mediaType string) ([]TMDBGenre, error) {
	if mediaType != "movie" && mediaType != "tv" {
		return nil, fmt.Errorf("%w: genre media type must be movie or tv", httpx.ErrValidation)
	}
	c.mu.Lock()
	cached := c.genres[mediaType]
	c.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	var resp struct {
		Genres []TMDBGenre `json:"genres"`
	}
	if err := c.get(ctx, "/genre/"+mediaType+"/list", nil, &resp); err != nil {
		return nil, err
	}
	genres := resp.Genres
	if genres == nil {
		genres = []TMDBGenre{}
	}
	c.mu.Lock()
	if c.genres == nil {
		c.genres = map[string][]TMDBGenre{}
	}
	c.genres[mediaType] = genres
	c.mu.Unlock()
	return genres, nil
}

// Languages returns /configuration/languages, cached like Genres.
func (c *TMDB) Languages(ctx context.Context) ([]TMDBLanguage, error) {
	c.mu.Lock()
	cached := c.languages
	c.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	var resp []TMDBLanguage
	if err := c.get(ctx, "/configuration/languages", nil, &resp); err != nil {
		return nil, err
	}
	if resp == nil {
		resp = []TMDBLanguage{}
	}
	c.mu.Lock()
	c.languages = resp
	c.mu.Unlock()
	return resp, nil
}

// WatchProviders returns the streaming services TMDB lists for one region and
// kind — the Discover page's provider filter options — cached per (kind,
// region) like Genres. The region here is a filter on TMDB's own list of
// services, not an item lookup.
func (c *TMDB) WatchProviders(ctx context.Context, mediaType, region string) ([]TMDBWatchProviderOption, error) {
	if mediaType != "movie" && mediaType != "tv" {
		return nil, fmt.Errorf("%w: watch provider media type must be movie or tv", httpx.ErrValidation)
	}
	if region == "" {
		return nil, fmt.Errorf("%w: watch providers need a region", httpx.ErrValidation)
	}
	key := mediaType + ":" + region
	c.mu.Lock()
	cached := c.watchProviders[key]
	c.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	var resp struct {
		Results []TMDBWatchProviderOption `json:"results"`
	}
	if err := c.get(ctx, "/watch/providers/"+mediaType, url.Values{"watch_region": {region}}, &resp); err != nil {
		return nil, err
	}
	providers := resp.Results
	if providers == nil {
		providers = []TMDBWatchProviderOption{}
	}
	c.mu.Lock()
	if c.watchProviders == nil {
		c.watchProviders = map[string][]TMDBWatchProviderOption{}
	}
	c.watchProviders[key] = providers
	c.mu.Unlock()
	return providers, nil
}

// TMDBGenre is one entry of a movie's genres list.
type TMDBGenre struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TMDBMovieDetails is the /movie/{id} response, promoted fields only. The
// caller also receives the raw body, which carries everything else (credits
// included) and is what gets stored as the local metadata snapshot.
type TMDBMovieDetails struct {
	ID            int64       `json:"id"`
	Title         string      `json:"title"`
	OriginalTitle string      `json:"original_title"`
	ReleaseDate   string      `json:"release_date"` // YYYY-MM-DD, or "" when unknown
	Runtime       int         `json:"runtime"`      // minutes; 0 when TMDB has none
	Overview      string      `json:"overview"`
	Tagline       string      `json:"tagline"`
	Genres        []TMDBGenre `json:"genres"`
	PosterPath    string      `json:"poster_path"`   // "/abc.jpg", or "" when absent
	BackdropPath  string      `json:"backdrop_path"` // "/abc.jpg", or "" when absent
	VoteAverage   float64     `json:"vote_average"`
	VoteCount     int64       `json:"vote_count"`
}

// MovieDetails fetches /movie/{id} with credits, external ids, videos, the
// image galleries, the watch providers and the recommendations appended, so one
// call carries everything the details snapshot needs — "where to watch" (D-022)
// and "similar movies" (D-024) both ride this call rather than becoming further
// provider call sites. raw is the verbatim response body — the full metadata snapshot the
// caller stores; the appended sections are parsed out of it rather than declared
// here, so the same parser works on a fresh fetch and on a snapshot read back
// from the database. An unknown id returns httpx.ErrNotFound.
//
// include_image_language bounds what "images" costs. Left unset, TMDB filters the
// galleries by the request language and drops the textless backdrops the media
// section wants most; set to "*" it returns every localisation and the stored
// snapshot balloons. "en,null" is English plus textless — what a gallery renders.
func (c *TMDB) MovieDetails(ctx context.Context, tmdbID int64) (TMDBMovieDetails, json.RawMessage, error) {
	path := "/movie/" + strconv.FormatInt(tmdbID, 10)
	query := url.Values{
		"append_to_response":     {"credits,external_ids,videos,images,watch/providers,recommendations"},
		"include_image_language": {"en,null"},
	}
	var raw json.RawMessage
	if err := c.get(ctx, path, query, &raw); err != nil {
		return TMDBMovieDetails{}, nil, err
	}
	var details TMDBMovieDetails
	if err := json.Unmarshal(raw, &details); err != nil {
		return TMDBMovieDetails{}, nil, fmt.Errorf("%w: decoding tmdb %s response: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	return details, raw, nil
}

// TMDBTVSeason is one entry of a show's seasons array. TMDB returns these on
// the show root, so a details view gets every season without a call per season.
type TMDBTVSeason struct {
	SeasonNumber int     `json:"season_number"` // 0 is Specials
	Name         string  `json:"name"`
	EpisodeCount int     `json:"episode_count"`
	AirDate      string  `json:"air_date"` // YYYY-MM-DD, or "" when unknown
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"` // "/abc.jpg", or "" when absent
	VoteAverage  float64 `json:"vote_average"`
}

// TMDBTVEpisodeStub is the shape of next_episode_to_air / last_episode_to_air:
// enough to name an episode, not the full episode record.
type TMDBTVEpisodeStub struct {
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"` // YYYY-MM-DD, or "" when unknown
}

// TMDBTVDetails is the /tv/{id} response, promoted fields only. TMDB names a
// show's fields differently from a movie's (name/first_air_date, an
// episode_run_time array rather than one runtime, no budget or revenue), so
// this is a separate type rather than a lossy shared one — same reasoning as
// TMDBTVResult. The caller also receives the raw body, which carries the
// appended credits, external ids, videos and images.
type TMDBTVDetails struct {
	ID             int64       `json:"id"`
	Name           string      `json:"name"`
	OriginalName   string      `json:"original_name"`
	FirstAirDate   string      `json:"first_air_date"` // YYYY-MM-DD, or "" when unknown
	LastAirDate    string      `json:"last_air_date"`  // YYYY-MM-DD, or "" while unaired
	EpisodeRunTime []int       `json:"episode_run_time"`
	SeasonCount    int         `json:"number_of_seasons"`
	EpisodeCount   int         `json:"number_of_episodes"`
	Status         string      `json:"status"` // "Returning Series" | "Ended" | "Canceled" | …
	InProduction   bool        `json:"in_production"`
	Overview       string      `json:"overview"`
	Tagline        string      `json:"tagline"`
	Genres         []TMDBGenre `json:"genres"`
	PosterPath     string      `json:"poster_path"`   // "/abc.jpg", or "" when absent
	BackdropPath   string      `json:"backdrop_path"` // "/abc.jpg", or "" when absent
	VoteAverage    float64     `json:"vote_average"`
	VoteCount      int64       `json:"vote_count"`

	CreatedBy []struct {
		Name string `json:"name"`
	} `json:"created_by"`
	Networks []struct {
		Name string `json:"name"`
	} `json:"networks"`
	Seasons          []TMDBTVSeason     `json:"seasons"`
	NextEpisodeToAir *TMDBTVEpisodeStub `json:"next_episode_to_air"`
}

// TVDetails fetches /tv/{id} with credits, external ids, videos, the image
// galleries, the watch providers and the recommendations appended, so one call
// carries everything a show's details view needs — the seasons array rides the
// show root, so there is no call per season, and "where to watch" (D-022) and
// "similar shows" (D-024) ride it too. raw is the verbatim response body, parsed for the appended sections
// by the caller. An unknown id returns httpx.ErrNotFound.
//
// aggregate_credits, not credits: TMDB's tv `credits` is "the latest season
// credits of a TV show" — the current series regulars, four or five people for
// a show its own site lists 150 for. `aggregate_credits` is everyone credited
// across every episode, each with a roles array (character + episode_count) and
// a total_episode_count, which is what TMDB's "Series Cast" page renders. The
// shape differs from a movie's credits, so the show projection has its own
// reader (video.showCastEntries) rather than sharing the movie one.
// include_image_language is bounded exactly as MovieDetails explains.
func (c *TMDB) TVDetails(ctx context.Context, tmdbID int64) (TMDBTVDetails, json.RawMessage, error) {
	path := "/tv/" + strconv.FormatInt(tmdbID, 10)
	query := url.Values{
		"append_to_response":     {"aggregate_credits,external_ids,videos,images,watch/providers,recommendations"},
		"include_image_language": {"en,null"},
	}
	var raw json.RawMessage
	if err := c.get(ctx, path, query, &raw); err != nil {
		return TMDBTVDetails{}, nil, err
	}
	var details TMDBTVDetails
	if err := json.Unmarshal(raw, &details); err != nil {
		return TMDBTVDetails{}, nil, fmt.Errorf("%w: decoding tmdb %s response: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	return details, raw, nil
}

// TMDBTVSeasonEpisode is one entry of a season's episodes array — the season
// details page's episode list. Unlike the show root's seasons array, the
// season document carries the full episode records.
type TMDBTVSeasonEpisode struct {
	EpisodeNumber int    `json:"episode_number"`
	Name          string `json:"name"`
	Overview      string `json:"overview"`
	AirDate       string `json:"air_date"`   // YYYY-MM-DD, or "" when unknown
	Runtime       int    `json:"runtime"`    // minutes; 0 when TMDB has none
	StillPath     string `json:"still_path"` // "/abc.jpg", or "" when absent
	// TMDB's editorial label: "standard" | "mid_season" | "finale". There is
	// no premiere type — a premiere is episode_number 1.
	EpisodeType string  `json:"episode_type"`
	VoteAverage float64 `json:"vote_average"`
	VoteCount   int64   `json:"vote_count"`
	// GuestStars is who was credited for THIS episode beyond the season's
	// regulars — the episode page's second cast rail. It rides the season
	// document as TMDB sends it; no append is needed for it.
	GuestStars []TMDBCastCredit `json:"guest_stars"`
}

// TMDBCastCredit is the plain credits shape — one credited actor with one
// part — shared by a season's `credits` cast and an episode's guest stars. The
// aggregate shape (roles + episode counts) is parsed by the video feature.
type TMDBCastCredit struct {
	// ID is TMDB's person id — what the person page is addressed by. 0 when
	// a body carries none, which a real TMDB response never does.
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	ProfilePath string `json:"profile_path"` // "/abc.jpg", or "" when absent
	Order       int    `json:"order"`
}

// TMDBTVSeasonDetails is one season's document, promoted fields only. There
// is no season id: the appended "season/{n}" sub-document carries only a
// string _id (unlike the standalone season endpoint), and nothing here needs
// one — a season is addressed by (show id, season_number).
type TMDBTVSeasonDetails struct {
	SeasonNumber int                   `json:"season_number"` // 0 is Specials
	Name         string                `json:"name"`
	Overview     string                `json:"overview"`
	AirDate      string                `json:"air_date"`    // YYYY-MM-DD, or "" when unknown
	PosterPath   string                `json:"poster_path"` // "/abc.jpg", or "" when absent
	VoteAverage  float64               `json:"vote_average"`
	Episodes     []TMDBTVSeasonEpisode `json:"episodes"`
}

// TVSeasonDetails fetches one season plus the show root in a single request:
// GET /tv/{id}?append_to_response=season/{n},season/{n}/aggregate_credits,season/{n}/credits.
// The season document itself does not name its show, so the root supplies
// showTitle; raw is the SEASON sub-document — what the caller stores as its
// metadata snapshot, so a snapshot read back from the database parses like a
// fresh fetch.
//
// raw is not quite verbatim: TMDB returns each nested append as a sibling
// top-level key ("season/2/aggregate_credits", "season/2/credits"), and storing
// the sub-document alone would discard them, so both are folded into the season
// document under `aggregate_credits` and `credits` — the shape the standalone
// season endpoint returns with the same appends. `aggregate_credits` is the
// season's OWN cast (everyone credited in its episodes, with per-season episode
// counts) and `credits` its regular cast — the people TMDB's episode pages list
// as the cast of every episode of the season; the episode-specific half, each
// episode's guest_stars, is in the document already and needs no append. Both
// keys are always written: `null` when TMDB sent no section, so a stored row
// can be told apart from one written before the append existed, and from one
// where TMDB answered nobody ({"cast": []}).
//
// An unknown show is TMDB's own 404 → httpx.ErrNotFound; a known show whose
// response lacks the "season/{n}" key (season number out of range) maps to
// ErrNotFound too.
func (c *TMDB) TVSeasonDetails(ctx context.Context, showID int64, seasonNumber int) (TMDBTVSeasonDetails, string, json.RawMessage, error) {
	path := "/tv/" + strconv.FormatInt(showID, 10)
	seasonKey := "season/" + strconv.Itoa(seasonNumber)
	aggregateKey := seasonKey + "/aggregate_credits"
	creditsKey := seasonKey + "/credits"
	query := url.Values{
		"append_to_response": {seasonKey + "," + aggregateKey + "," + creditsKey},
	}
	var body map[string]json.RawMessage
	if err := c.get(ctx, path, query, &body); err != nil {
		return TMDBTVSeasonDetails{}, "", nil, err
	}

	var showTitle string
	if nameRaw, ok := body["name"]; ok {
		if err := json.Unmarshal(nameRaw, &showTitle); err != nil {
			return TMDBTVSeasonDetails{}, "", nil, fmt.Errorf("%w: decoding tmdb %s response: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
		}
	}

	seasonRaw, ok := body[seasonKey]
	if !ok || string(seasonRaw) == "null" {
		return TMDBTVSeasonDetails{}, "", nil, fmt.Errorf("%w: tmdb show %d has no season %d", httpx.ErrNotFound, showID, seasonNumber)
	}
	var season TMDBTVSeasonDetails
	if err := json.Unmarshal(seasonRaw, &season); err != nil {
		return TMDBTVSeasonDetails{}, "", nil, fmt.Errorf("%w: decoding tmdb %s season response: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}

	// Fold the nested appends into the season document (see the doc comment).
	var seasonDoc map[string]json.RawMessage
	if err := json.Unmarshal(seasonRaw, &seasonDoc); err != nil {
		return TMDBTVSeasonDetails{}, "", nil, fmt.Errorf("%w: decoding tmdb %s season response: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	for appended, stored := range map[string]string{aggregateKey: "aggregate_credits", creditsKey: "credits"} {
		section, ok := body[appended]
		if !ok {
			section = json.RawMessage("null")
		}
		seasonDoc[stored] = section
	}
	merged, err := json.Marshal(seasonDoc)
	if err != nil {
		return TMDBTVSeasonDetails{}, "", nil, fmt.Errorf("%w: encoding tmdb %s season snapshot: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	return season, showTitle, merged, nil
}

// TMDBPersonCastCredit is one entry of a person's combined_credits.cast. TMDB
// lists a title once PER CHARACTER — an actor with two parts in one show
// appears twice, each entry carrying that part's episode_count — so the video
// feature deduplicates on (media_type, id). media_type is TMDB's "movie" |
// "tv"; a movie names itself with title/release_date, a show with
// name/first_air_date.
type TMDBPersonCastCredit struct {
	ID           int64   `json:"id"`
	MediaType    string  `json:"media_type"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"release_date"`   // YYYY-MM-DD, or "" when unknown
	FirstAirDate string  `json:"first_air_date"` // YYYY-MM-DD, or "" when unknown
	Character    string  `json:"character"`
	PosterPath   string  `json:"poster_path"` // "/abc.jpg", or "" when absent
	VoteAverage  float64 `json:"vote_average"`
	EpisodeCount int     `json:"episode_count"` // shows only; 0 for a movie
	Order        int     `json:"order"`
}

// TMDBPersonDetails is /person/{id} with combined_credits and external_ids
// appended: everything the person page renders. Nullable strings arrive as
// "" — TMDB sends null for an unknown birthday or homepage, which decodes to
// the zero value.
type TMDBPersonDetails struct {
	ID                 int64    `json:"id"`
	Name               string   `json:"name"`
	Biography          string   `json:"biography"`
	KnownForDepartment string   `json:"known_for_department"` // "Acting", "Directing", …
	Birthday           string   `json:"birthday"`             // YYYY-MM-DD, or ""
	Deathday           string   `json:"deathday"`             // YYYY-MM-DD, or ""
	PlaceOfBirth       string   `json:"place_of_birth"`
	AlsoKnownAs        []string `json:"also_known_as"`
	Homepage           string   `json:"homepage"`
	ProfilePath        string   `json:"profile_path"` // "/abc.jpg", or "" when absent
	ExternalIDs        struct {
		IMDBID string `json:"imdb_id"` // "nm0000093", or ""
	} `json:"external_ids"`
	CombinedCredits struct {
		Cast []TMDBPersonCastCredit `json:"cast"`
	} `json:"combined_credits"`
}

// PersonDetails fetches one person with their acting credits and external ids
// in a single request: GET /person/{id}?append_to_response=combined_credits,external_ids.
//
// Unlike MovieDetails and TVDetails there is no raw body return, on purpose:
// nothing stores a person (D-027 — the person page is served live on every
// view and never becomes a catalog row), so there is no snapshot to keep the
// verbatim body for. An unknown id returns httpx.ErrNotFound.
func (c *TMDB) PersonDetails(ctx context.Context, personID int64) (TMDBPersonDetails, error) {
	path := "/person/" + strconv.FormatInt(personID, 10)
	query := url.Values{"append_to_response": {"combined_credits,external_ids"}}
	var details TMDBPersonDetails
	if err := c.get(ctx, path, query, &details); err != nil {
		return TMDBPersonDetails{}, err
	}
	return details, nil
}

// DownloadImage fetches one image variant from TMDB's image CDN. size is a
// provider variant name ("w185", "w500", …) — callers whitelist it; path is
// TMDB's image path as returned by search, e.g. "/abc.jpg".
//
// The CDN itself takes no credential, but the base URL comes from
// /configuration, so an unconfigured client fails the same way search does. An
// upstream 404 maps to httpx.ErrNotFound — a stale filename is the client's
// mistake, not a provider outage.
func (c *TMDB) DownloadImage(ctx context.Context, size, path string) ([]byte, error) {
	cfg, err := c.Configuration(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.limit.wait(ctx); err != nil {
		return nil, fmt.Errorf("%w: tmdb image %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}

	target := strings.TrimRight(cfg.SecureBaseURL, "/") + "/" + size + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: tmdb image %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: tmdb image %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: tmdb has no image %s at size %s", httpx.ErrNotFound, path, size)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: tmdb image %s returned %s", httpx.ErrProviderUnavailable, path, redact(resp.Status))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, tmdbMaxImage))
	if err != nil {
		return nil, fmt.Errorf("%w: reading tmdb image %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	return data, nil
}

// get performs one rate-limited, authenticated GET and decodes the JSON body.
// It is send with the method fixed; every read in this file goes through it.
func (c *TMDB) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.send(ctx, http.MethodGet, path, query, nil, out)
}

// tmdbStatusInvalidKey is TMDB's status_code for a rejected application key.
// Every other 401 on a request that carried a user session (or exchanged a
// request token for one) means the SESSION is the problem, not the key.
const tmdbStatusInvalidKey = 7

// send performs one rate-limited, authenticated request and decodes the JSON
// body when out is non-nil. body, when non-nil, is JSON-encoded — the shape
// TMDB's write endpoints take (D-029).
//
// Errors name the request path rather than the full URL, because on the v3 auth
// path the URL carries the key — and a user's session id rides the query too;
// redact is the second line of defence for error text produced by net/http
// itself, and for a credential TMDB echoes back.
//
// Status mapping: 2xx decodes (TMDB answers 201 on a first rating, 200 on an
// update or a delete); 401 is the application key unless the request carried a
// session or was the session exchange, in which case it is ErrSessionInvalid;
// 404 is httpx.ErrNotFound; 400 and 422 are httpx.ErrValidation (the request
// is wrong, TMDB is fine); anything else is an outage.
func (c *TMDB) send(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if !c.Configured() {
		return fmt.Errorf("%w: no TMDB credential configured — set TMDB_API_KEY_V3 or TMDB_API_KEY_V4", httpx.ErrProviderUnavailable)
	}
	if err := c.limit.wait(ctx); err != nil {
		return fmt.Errorf("%w: tmdb %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}

	if query == nil {
		query = url.Values{}
	}
	userSession := query.Get("session_id") != "" || path == tmdbSessionNewPath
	if c.auth == authQueryV3 {
		query.Set("api_key", c.credential)
	}

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%w: encoding tmdb %s request: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
		}
		payload = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return fmt.Errorf("%w: tmdb %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		// TMDB's documented content type for its write endpoints, charset
		// included — a bare application/json is refused by some of them.
		req.Header.Set("Content-Type", "application/json;charset=utf-8")
	}
	if c.auth == authBearerV4 {
		req.Header.Set("Authorization", "Bearer "+c.credential)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: tmdb %s: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// TMDB puts a status_message in the body; 401 means the key is wrong,
		// which is the failure worth naming precisely.
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		detail := strings.TrimSpace(string(raw))
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			if userSession && tmdbStatusCode(raw) != tmdbStatusInvalidKey {
				return fmt.Errorf("%w: tmdb refused the user session on %s", ErrSessionInvalid, path)
			}
			return fmt.Errorf("%w: tmdb rejected the API key (401) on %s: %s", httpx.ErrProviderUnavailable, path, redact(detail))
		case http.StatusNotFound:
			// A 404 means the requested resource does not exist — a bogus id from
			// the client, not a provider outage. Mirrors DownloadImage.
			return fmt.Errorf("%w: tmdb has no resource at %s", httpx.ErrNotFound, path)
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return fmt.Errorf("%w: tmdb refused the request to %s: %s", httpx.ErrValidation, path, redact(detail))
		}
		return fmt.Errorf("%w: tmdb %s returned %s: %s", httpx.ErrProviderUnavailable, path, resp.Status, redact(detail))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, tmdbMaxBody)).Decode(out); err != nil {
		return fmt.Errorf("%w: decoding tmdb %s response: %s", httpx.ErrProviderUnavailable, path, redact(err.Error()))
	}
	return nil
}

// tmdbStatusCode reads the status_code out of a TMDB error envelope; 0 when
// the body is not one.
func tmdbStatusCode(raw []byte) int {
	var env struct {
		StatusCode int `json:"status_code"`
	}
	if json.Unmarshal(raw, &env) != nil {
		return 0
	}
	return env.StatusCode
}
