package provider

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"mbyte.ro/sofalog/httpx"
)

// The user-session half of the TMDB client (D-029): linking a sofalog user to
// their own TMDB account, and pushing that user's ratings — and, on request,
// watchlist changes — to it. These are the
// only WRITES this package performs against a provider. Every call carries the
// application credential as usual plus the user's session_id — TMDB's v3
// scheme, where the session is a query parameter.
//
// The read surface is deliberately small. Account is called once at link time
// for the username shown on the Settings page, and the five Account* list
// readers below serve the ONE-TIME setup pull (D-036): the account's rated,
// watchlist and favorite lists, read once when the user asks for them after
// linking and never again — local stays authoritative afterwards.

const (
	// tmdbAuthenticateURL is where the browser is sent to approve a request
	// token. Built here, not in a handler: /verify forbids handlers*.go from
	// naming the TMDB host.
	tmdbAuthenticateURL = "https://www.themoviedb.org/authenticate/"

	tmdbSessionNewPath = "/authentication/session/new"
	tmdbSessionPath    = "/authentication/session"
)

// TMDBAccount is the linked account's identity, read once at link time.
type TMDBAccount struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// RatingTarget addresses one rateable thing on TMDB. Kind is movie, show or
// episode; TMDBID is the movie's or the SHOW's id — an episode is addressed by
// its show plus its position, because TMDB publishes no episode-id route for
// ratings (and the catalog stores none, see the episodes DDL).
type RatingTarget struct {
	Kind    string
	TMDBID  int64
	Season  int
	Episode int
}

// NewRequestToken starts the link flow: a token the user approves on TMDB's
// site, valid for an hour there.
func (c *TMDB) NewRequestToken(ctx context.Context) (string, error) {
	var out struct {
		RequestToken string `json:"request_token"`
	}
	if err := c.get(ctx, "/authentication/token/new", nil, &out); err != nil {
		return "", err
	}
	if out.RequestToken == "" {
		return "", fmt.Errorf("%w: tmdb returned no request token", httpx.ErrProviderUnavailable)
	}
	return out.RequestToken, nil
}

// AuthenticateURL is the page the browser is sent to approve requestToken;
// TMDB redirects back to redirectTo with request_token and approved=true (or
// denied=true) in the query.
func (c *TMDB) AuthenticateURL(requestToken, redirectTo string) string {
	return tmdbAuthenticateURL + url.PathEscape(requestToken) + "?redirect_to=" + url.QueryEscape(redirectTo)
}

// CreateSession exchanges an approved request token for the long-lived
// session id. An unapproved or expired token is ErrSessionInvalid.
func (c *TMDB) CreateSession(ctx context.Context, requestToken string) (string, error) {
	body := map[string]string{"request_token": requestToken}
	var out struct {
		SessionID string `json:"session_id"`
	}
	if err := c.send(ctx, "POST", tmdbSessionNewPath, nil, body, &out); err != nil {
		return "", err
	}
	if out.SessionID == "" {
		return "", fmt.Errorf("%w: tmdb returned no session id", httpx.ErrProviderUnavailable)
	}
	return out.SessionID, nil
}

// DeleteSession revokes a session at TMDB — the unlink's last step. Best
// effort at the call site: a session TMDB no longer knows is already revoked.
func (c *TMDB) DeleteSession(ctx context.Context, sessionID string) error {
	body := map[string]string{"session_id": sessionID}
	return c.send(ctx, "DELETE", tmdbSessionPath, nil, body, nil)
}

// Account reads the linked account's id and username.
func (c *TMDB) Account(ctx context.Context, sessionID string) (TMDBAccount, error) {
	var out TMDBAccount
	err := c.get(ctx, "/account", url.Values{"session_id": {sessionID}}, &out)
	return out, err
}

// SetRating writes value (1-10; TMDB accepts 0.5 steps, integers are a subset)
// as the user's rating of t. TMDB answers 201 on a first rating and 200 on an
// overwrite; both are success here.
func (c *TMDB) SetRating(ctx context.Context, sessionID string, t RatingTarget, value int) error {
	path, err := ratingPath(t)
	if err != nil {
		return err
	}
	body := map[string]int{"value": value}
	return c.send(ctx, "POST", path, url.Values{"session_id": {sessionID}}, body, nil)
}

// DeleteRating removes the user's rating of t.
func (c *TMDB) DeleteRating(ctx context.Context, sessionID string, t RatingTarget) error {
	path, err := ratingPath(t)
	if err != nil {
		return err
	}
	return c.send(ctx, "DELETE", path, url.Values{"session_id": {sessionID}}, nil, nil)
}

// SetWatchlist adds (on) or removes (!on) a movie or show from the account's
// TMDB watchlist. kind is sofalog's target type; TMDB's media_type for a show
// is "tv". Both directions answer 2xx (status_code 1 or 12 on add, 13 on
// remove), and removing what is not listed is still a success.
func (c *TMDB) SetWatchlist(ctx context.Context, sessionID string, accountID int64, kind string, tmdbID int64, on bool) error {
	mediaType, err := accountMediaType(kind)
	if err != nil {
		return err
	}
	body := map[string]any{"media_type": mediaType, "media_id": tmdbID, "watchlist": on}
	path := "/account/" + strconv.FormatInt(accountID, 10) + "/watchlist"
	return c.send(ctx, "POST", path, url.Values{"session_id": {sessionID}}, body, nil)
}

// SetFavorite adds (on) or removes (!on) a movie or show from the account's
// TMDB favorites — the watchlist call's twin (D-030): same account route
// family, same body shape with `favorite` in place of `watchlist`, same
// status codes, and removing what is not listed is still a success.
func (c *TMDB) SetFavorite(ctx context.Context, sessionID string, accountID int64, kind string, tmdbID int64, on bool) error {
	mediaType, err := accountMediaType(kind)
	if err != nil {
		return err
	}
	body := map[string]any{"media_type": mediaType, "media_id": tmdbID, "favorite": on}
	path := "/account/" + strconv.FormatInt(accountID, 10) + "/favorite"
	return c.send(ctx, "POST", path, url.Values{"session_id": {sessionID}}, body, nil)
}

// accountMediaType maps a sofalog target type onto TMDB's media_type for the
// account lists (watchlist and favorites), which take movies and shows only.
func accountMediaType(kind string) (string, error) {
	switch kind {
	case "movie":
		return "movie", nil
	case "show":
		return "tv", nil
	}
	return "", fmt.Errorf("%w: tmdb has no account list entry for %q", httpx.ErrValidation, kind)
}

// ratingPath is the one place a RatingTarget becomes a TMDB route.
func ratingPath(t RatingTarget) (string, error) {
	id := strconv.FormatInt(t.TMDBID, 10)
	switch t.Kind {
	case "movie":
		return "/movie/" + id + "/rating", nil
	case "show":
		return "/tv/" + id + "/rating", nil
	case "episode":
		return "/tv/" + id + "/season/" + strconv.Itoa(t.Season) + "/episode/" + strconv.Itoa(t.Episode) + "/rating", nil
	}
	return "", fmt.Errorf("%w: tmdb has no rating route for %q", httpx.ErrValidation, t.Kind)
}

// TMDBRatedItem is one entry of /account/{id}/rated/movies or /rated/tv: the
// item's TMDB id and the user's rating, which TMDB stores in 0.5 steps.
type TMDBRatedItem struct {
	ID     int64   `json:"id"`
	Rating float64 `json:"rating"`
}

// TMDBRatedEpisode is one entry of /account/{id}/rated/tv/episodes. TMDB
// addresses it by show id plus position, which is also how the catalog does.
type TMDBRatedEpisode struct {
	ShowID  int64   `json:"show_id"`
	Season  int     `json:"season_number"`
	Episode int     `json:"episode_number"`
	Rating  float64 `json:"rating"`
}

// TMDBListedItem is one entry of the account's watchlist or favorite lists.
type TMDBListedItem struct {
	ID int64 `json:"id"`
}

// The three account list pages share TMDB's paging envelope.
type TMDBRatedPage struct {
	Page       int             `json:"page"`
	TotalPages int             `json:"total_pages"`
	Results    []TMDBRatedItem `json:"results"`
}

type TMDBRatedEpisodePage struct {
	Page       int                `json:"page"`
	TotalPages int                `json:"total_pages"`
	Results    []TMDBRatedEpisode `json:"results"`
}

type TMDBListedPage struct {
	Page       int              `json:"page"`
	TotalPages int              `json:"total_pages"`
	Results    []TMDBListedItem `json:"results"`
}

// AccountRatedMovies reads one page of the movies the account has rated.
func (c *TMDB) AccountRatedMovies(ctx context.Context, sessionID string, accountID int64, page int) (TMDBRatedPage, error) {
	var out TMDBRatedPage
	err := c.accountList(ctx, sessionID, accountID, "rated/movies", page, &out)
	return out, err
}

// AccountRatedShows reads one page of the shows the account has rated.
func (c *TMDB) AccountRatedShows(ctx context.Context, sessionID string, accountID int64, page int) (TMDBRatedPage, error) {
	var out TMDBRatedPage
	err := c.accountList(ctx, sessionID, accountID, "rated/tv", page, &out)
	return out, err
}

// AccountRatedEpisodes reads one page of the episodes the account has rated.
func (c *TMDB) AccountRatedEpisodes(ctx context.Context, sessionID string, accountID int64, page int) (TMDBRatedEpisodePage, error) {
	var out TMDBRatedEpisodePage
	err := c.accountList(ctx, sessionID, accountID, "rated/tv/episodes", page, &out)
	return out, err
}

// AccountWatchlist reads one page of the account's watchlist for one kind
// (sofalog's movie or show).
func (c *TMDB) AccountWatchlist(ctx context.Context, sessionID string, accountID int64, kind string, page int) (TMDBListedPage, error) {
	suffix, err := accountListSuffix(kind)
	if err != nil {
		return TMDBListedPage{}, err
	}
	var out TMDBListedPage
	err = c.accountList(ctx, sessionID, accountID, "watchlist/"+suffix, page, &out)
	return out, err
}

// AccountFavorites reads one page of the account's favorites for one kind.
func (c *TMDB) AccountFavorites(ctx context.Context, sessionID string, accountID int64, kind string, page int) (TMDBListedPage, error) {
	suffix, err := accountListSuffix(kind)
	if err != nil {
		return TMDBListedPage{}, err
	}
	var out TMDBListedPage
	err = c.accountList(ctx, sessionID, accountID, "favorite/"+suffix, page, &out)
	return out, err
}

// accountList is the one request behind the five account list readers:
// /account/{id}/{suffix} with the session and the page in the query. The page
// is checked before the request, like Trending's. Error mapping is send's —
// a 401 for the session is ErrSessionInvalid, anything else an outage.
func (c *TMDB) accountList(ctx context.Context, sessionID string, accountID int64, suffix string, page int, out any) error {
	if page < 1 || page > tmdbMaxPage {
		return fmt.Errorf("%w: page must be between 1 and %d", httpx.ErrValidation, tmdbMaxPage)
	}
	path := "/account/" + strconv.FormatInt(accountID, 10) + "/" + suffix
	query := url.Values{"session_id": {sessionID}, "page": {strconv.Itoa(page)}}
	return c.get(ctx, path, query, out)
}

// accountListSuffix maps a sofalog kind onto the account list path segment:
// TMDB pluralises movies ("movies") but not tv ("tv").
func accountListSuffix(kind string) (string, error) {
	switch kind {
	case "movie":
		return "movies", nil
	case "show":
		return "tv", nil
	}
	return "", fmt.Errorf("%w: tmdb has no account list for %q", httpx.ErrValidation, kind)
}
