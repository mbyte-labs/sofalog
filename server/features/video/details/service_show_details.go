package details

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// ShowDetailsParams carries the raw query strings of GET /video/shows/details;
// the service owns validation. Exactly one of ID and TMDBID, exactly as the
// movie view: ID is the local shows.id a tracked show has, TMDBID addresses a
// show that may not be tracked at all. D-013 keeps the two namespaces apart by
// parameter name.
type ShowDetailsParams struct {
	ID     string
	TMDBID string
}

// ShowSeason is one season on the details page's seasons grid. Three fields
// are this user's: Copies (owned copies live per-season, D-021), and the two
// derived reads the poster badges — Progress, the season's watched/aired/total
// counts, and UserRating, the average of the user's episode ratings in it (a
// season is not itself rateable, D-020, so this is the one "your rating" a
// season can show). Everything else is the snapshot's; there is no user status,
// because a season has none (D-010).
type ShowSeason struct {
	// ID is the local seasons.id once a season details view has materialised a
	// snapshot row, or null before then. The grid links by it when present,
	// else by (show tmdb_id, season_number).
	ID           *int64   `json:"id"`
	SeasonNumber int      `json:"season_number"` // 0 is Specials
	Name         string   `json:"name"`
	EpisodeCount int      `json:"episode_count"`
	AirDate      *string  `json:"air_date"` // YYYY-MM-DD
	Year         *string  `json:"year"`     // "2008", derived from AirDate
	Overview     *string  `json:"overview"`
	Rating       *float64 `json:"rating"` // TMDB's season score, null when unrated
	// Relative URLs of the locally proxied poster variants, never a provider
	// URL. Null when TMDB has no season poster.
	Poster     *string `json:"poster"`
	PosterFull *string `json:"poster_full"`
	// The kinds this user holds a copy of THIS season on (D-021). Always
	// present; [] when none, and always [] for an untracked show, which has no
	// season rows to hold copies.
	Copies []string `json:"copies"`
	// JellyfinEpisodes is how many of this season's episode files the Jellyfin
	// library scan found (D-033), null when the scan does not own the jellyfin
	// kind or the season is not on the server. The page compares it with the
	// season's aired episodes to colour the tile: complete or missing some.
	JellyfinEpisodes *int `json:"jellyfin_episodes"`
	// Progress is this season's derived progress (D-010) — the show's three
	// counts narrowed to one season, Specials counting their own episodes.
	// Null for an untracked show, which has no episode rows to count.
	Progress *common.ShowProgressCounts `json:"progress"`
	// UserRating is the average of this user's EPISODE ratings in the season,
	// null when none is rated or the show is untracked. A float, unlike the
	// integer ratings elsewhere: it is derived, never stored (D-020).
	UserRating *float64 `json:"user_rating"`
}

// ShowEpisodeStub names one episode without carrying its full record — enough
// for the "next episode" fact. Null when TMDB knows of no upcoming episode.
type ShowEpisodeStub struct {
	Season  int     `json:"season"`
	Number  int     `json:"number"`
	Title   *string `json:"title"`
	AirDate *string `json:"air_date"` // YYYY-MM-DD
}

// ShowDetails is one show, always served live from TMDB in this slice (D-015).
// It mirrors MovieDetails wherever the two concepts match, so the SPA's detail
// pages share their components, and diverges where TV does: seasons and episode
// counts instead of a runtime and a certification, created_by instead of a
// director, networks instead of budget and revenue.
//
// CastMember, MovieVideo and MovieImage are reused verbatim — a show's credits
// and artwork carry exactly the movie shapes, and a parallel set of types would
// differ only in name.
//
// Nullable fields are pointers without omitempty — the SPA distinguishes null
// from absent.
type ShowDetails struct {
	// ID is the local shows.id once the show has been tracked, and null while
	// it is only being looked at — a details view of an untracked show still
	// creates no catalog row (D-015). The SPA adopts it from a tracking
	// response and addresses the show by it afterwards.
	ID     *int64 `json:"id"`
	TMDBID int64  `json:"tmdb_id"`
	// Title is TMDB's "name". The SPA renders movies and shows through the same
	// components, so the field keeps the movie name.
	Title         string  `json:"title"`
	OriginalTitle *string `json:"original_title"`
	FirstAirDate  *string `json:"first_air_date"` // YYYY-MM-DD
	LastAirDate   *string `json:"last_air_date"`  // YYYY-MM-DD
	Year          *string `json:"year"`           // "2008", from FirstAirDate
	// EndYear is set only for a show that has finished airing — a returning
	// series has a last_air_date but no end year.
	EndYear *string `json:"end_year"`
	// EpisodeRuntimeMin is the show's typical episode length: TMDB reports an
	// array (a show can change format), and the first entry is the one its own
	// pages show.
	EpisodeRuntimeMin *int `json:"episode_runtime_min"`
	SeasonsCount      int  `json:"seasons_count"`
	EpisodesCount     int  `json:"episodes_count"`
	// ShowStatus is TMDB's production status ("Returning Series", "Ended",
	// "Canceled"). It is NOT the user's watch status — M2's user_show_states
	// carries watching|watched|dropped, a different axis that happens to share
	// the word.
	ShowStatus   *string  `json:"show_status"`
	InProduction bool     `json:"in_production"`
	Overview     *string  `json:"overview"`
	Tagline      *string  `json:"tagline"`
	Genres       []string `json:"genres"`
	// CreatedBy and Networks are the show analogues of a movie's director and
	// its budget/revenue facts.
	CreatedBy []string `json:"created_by"`
	Networks  []string `json:"networks"`

	NextEpisode *ShowEpisodeStub `json:"next_episode"`

	Rating *float64            `json:"rating"` // TMDB community score (0–10)
	Votes  *int64              `json:"votes"`
	Cast   []common.CastMember `json:"cast"`
	// Seasons carries every season TMDB lists, ordered ascending with Specials
	// (season 0) last. Capping is presentation, so the client owns it.
	Seasons []ShowSeason `json:"seasons"`

	Videos    []common.MovieVideo `json:"videos"`
	Backdrops []common.MovieImage `json:"backdrops"`
	Posters   []common.MovieImage `json:"posters"`

	Poster     *string `json:"poster"`
	PosterFull *string `json:"poster_full"`
	// The hero backdrop, downloaded for every tracked show (SPEC §5) and
	// therefore exposed so the file it downloads has a renderer.
	Backdrop         *string  `json:"backdrop"`
	BackdropFull     *string  `json:"backdrop_full"`
	Homepage         *string  `json:"homepage"`
	IMDBID           *string  `json:"imdb_id"`
	TrailerURL       *string  `json:"trailer_url"`
	OriginalLanguage *string  `json:"original_language"` // ISO 639-1
	Popularity       *float64 `json:"popularity"`
	FetchedAt        string   `json:"fetched_at"` // RFC3339 UTC

	// Tracking state. Tracked means the show has a catalog row; Watched is the
	// stored status having reached 'watched'. Watchlisted is this user's row in
	// user_video_watchlist — an independent axis, never a status (D-011), so it
	// is orthogonal to both of the others. Favorited is the third such axis,
	// this user's row in user_video_favorites (D-030).
	Tracked     bool `json:"tracked"`
	Watched     bool `json:"watched"`
	Watchlisted bool `json:"watchlisted"`
	Favorited   bool `json:"favorited"`
	// JellyfinSynced reports that a Jellyfin server is configured (D-033), so
	// every season's `jellyfin` copy is the scan's answer and the show page's
	// tile is read-only — see MovieDetails.JellyfinSynced.
	JellyfinSynced bool `json:"jellyfin_synced"`
	// Status is the system-maintained watch status — watching | watched |
	// dropped — or null when untracked. Distinct from ShowStatus above, which
	// is TMDB's production status.
	Status     *string `json:"status"`
	UserRating *int    `json:"user_rating"` // this user's 1-10 rating, null when unrated
	// UserEpisodeRating is the mean of this user's EPISODE ratings across the
	// whole show — the companion read under UserRating, weighted per episode
	// (a client mean of the seasons' means would weight seasons instead).
	// Null when none is rated or the show is untracked; UserRatedEpisodes is
	// how many ratings went into it, 0 whenever the mean is null.
	UserEpisodeRating *float64                   `json:"user_episode_rating"`
	UserRatedEpisodes int                        `json:"user_rated_episodes"`
	Progress          *common.ShowProgressCounts `json:"progress"` // derived, null when untracked
	// Where the show streams, rents or sells in the configured region
	// (D-022). Catalog data, not per-user, and the listing its seasons and
	// episodes borrow — TMDB publishes providers per show, never per season.
	// Null when the snapshot holds no listing at all; present-but-empty when
	// TMDB lists nobody in that country.
	Streaming *common.Streaming `json:"streaming"`
	// TMDB's recommendations for this show — the "Similar shows" rail (D-024).
	// Catalog data from the snapshot, like Streaming, and like it identical for
	// every user. Never null: [] both for a snapshot with no section yet and
	// for one where TMDB lists nobody. Seasons and episodes carry no rail —
	// TMDB publishes recommendations per show only, and unlike Streaming this
	// is not borrowed downward.
	Similar []common.SimilarTitle `json:"similar"`
}

// ShowDetails returns one show: from its stored snapshot when it is tracked,
// live from TMDB when it is not. Exactly the movie view's three branches
// (D-015), which shows only became able to have once they got a catalog row:
//
//   - id= with no row: a plain 404, never a TMDB call.
//   - either form with a row: serve the snapshot; one older than ShowStaleAfter
//     — or predating an appended section, or carrying a provider listing for
//     the wrong country (showSnapshotMissingSections) — is refreshed
//     synchronously, and if that refresh fails the stale snapshot is served and
//     the failure logged. A TRACKED show never 503s again.
//   - tmdb_id with no row: live from TMDB, storing nothing, and a 503 when TMDB
//     is down — there is no snapshot to fall back on. The untracked half of
//     D-015, unchanged.
//
// Both addressing forms fall through to ONE gate, exactly as MovieDetails does.
// They used to be two branches with the gate on the tmdb_id side only, which
// meant a show reached by its local id — how the watchlist links it, and how
// this page re-reads itself after any write — never refreshed at all: not on
// staleness, not on a widened append, not on a changed watch region.
func (s *Service) ShowDetails(ctx context.Context, userID int64, p ShowDetailsParams) (ShowDetails, error) {
	byID := p.ID != ""
	byTMDB := p.TMDBID != ""
	if byID == byTMDB {
		return ShowDetails{}, fmt.Errorf("%w: exactly one of id and tmdb_id is required", httpx.ErrValidation)
	}

	var row common.ShowRow
	if byID {
		id, err := strconv.ParseInt(p.ID, 10, 64)
		if err != nil || id < 1 {
			return ShowDetails{}, fmt.Errorf("%w: id must be a positive integer", httpx.ErrValidation)
		}
		row, err = common.GetShowByID(ctx, s.DB, id)
		if err != nil {
			return ShowDetails{}, err
		}
	} else {
		tmdbID, err := strconv.ParseInt(p.TMDBID, 10, 64)
		if err != nil || tmdbID < 1 {
			return ShowDetails{}, fmt.Errorf("%w: tmdb_id must be a positive integer", httpx.ErrValidation)
		}
		row, err = common.GetShowByTMDBID(ctx, s.DB, tmdbID)
		if errors.Is(err, httpx.ErrNotFound) {
			// Untracked: live, and storing nothing. The season-id read happens
			// after the fetch so an outage never turns into a different error.
			// An untracked show has no season rows and so no copies either —
			// the empty map renders every season's copies as [] — and no
			// episode rows, so its progress and rating average render null.
			details, raw, ferr := s.TMDB.TVDetails(ctx, tmdbID)
			if ferr != nil {
				return ShowDetails{}, ferr
			}
			seasonIDs, serr := common.SeasonIDsByShow(ctx, s.DB, tmdbID)
			if serr != nil {
				return ShowDetails{}, serr
			}
			return s.showDetailsFrom(details, raw, seasonIDs, map[int][]string{}, map[int]int{}, map[int]common.SeasonUserStats{}, common.AssetLinker{}), nil
		}
		if err != nil {
			return ShowDetails{}, err
		}
	}

	if s.showSnapshotStale(row.FetchedAt) || showSnapshotMissingSections(row.MetadataJSON, s.WatchRegion) {
		fresh, err := s.RefreshShow(ctx, row.TMDBID)
		if err != nil {
			// Serving slightly old data beats failing a page the library
			// can answer — the local-first contract (D-015).
			slog.Warn("tmdb: show refresh failed, serving stale snapshot",
				"show_id", row.ID, "tmdb_id", row.TMDBID, "fetched_at", row.FetchedAt, "err", err)
		} else {
			row = fresh
			s.SyncShowAssets(ctx, row)
		}
	}
	return s.showDetailsForRow(ctx, userID, row)
}

// showSnapshotStale mirrors movieSnapshotStale: an unparseable fetched_at
// counts as stale, so refreshing rewrites it — the self-repair path.
func (s *Service) showSnapshotStale(fetchedAt string) bool {
	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return true
	}
	return s.Now().Sub(t) > common.ShowStaleAfter
}

// showDetailsForRow projects a stored show row, filling in this user's private
// state. The snapshot is the verbatim /tv/{id} body, so it parses back into the
// same provider type a fresh fetch produces and one projection serves both.
func (s *Service) showDetailsForRow(ctx context.Context, userID int64, row common.ShowRow) (ShowDetails, error) {
	var details provider.TMDBTVDetails
	// Tolerant, like every other snapshot read: a corrupt body renders as a
	// sparse page rather than a 500.
	_ = json.Unmarshal([]byte(row.MetadataJSON), &details)

	seasonIDs, err := common.SeasonIDsByShow(ctx, s.DB, row.TMDBID)
	if err != nil {
		return ShowDetails{}, err
	}
	seasonCopies, jellyfinEpisodes, err := s.SeasonCopiesFor(ctx, userID, row.TMDBID)
	if err != nil {
		return ShowDetails{}, err
	}
	// One `today` for the show's progress and every season's: the two must
	// agree on what has aired, or the seasons could sum past the show.
	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return ShowDetails{}, err
	}
	today := clk.Today
	seasonStats, err := common.SeasonUserStatsByShow(ctx, s.DB, userID, row.ID, today)
	if err != nil {
		return ShowDetails{}, err
	}
	d := s.showDetailsFrom(details, json.RawMessage(row.MetadataJSON), seasonIDs, seasonCopies, jellyfinEpisodes,
		seasonStats, common.ShowLinker(row.ID, s.ShowFiles(row.ID)))

	// FetchedAt is the snapshot's, not now: the page dates what it is showing.
	d.FetchedAt = row.FetchedAt
	id := row.ID
	d.ID = &id
	d.Tracked = true

	state, err := common.UserShowFlags(ctx, s.DB, userID, row.ID)
	if err != nil {
		return ShowDetails{}, err
	}
	d.Status = state.Status
	d.Watched = state.Status != nil && *state.Status == "watched"
	d.Watchlisted = state.Watchlisted
	d.Favorited = state.Favorited
	d.UserRating = state.Rating
	d.UserEpisodeRating = state.EpisodeRatingAvg
	d.UserRatedEpisodes = state.RatedEpisodes

	progress, err := common.ShowProgress(ctx, s.DB, userID, row.ID, today)
	if err != nil {
		return ShowDetails{}, err
	}
	d.Progress = &progress
	return d, nil
}

// showDetailsFrom builds the DTO from a /tv/{id} response. The appended
// sections are parsed out of the raw body rather than the promoted struct, so
// the same MediaSnapshot parser serves movies and shows. seasonIDs maps season
// numbers to materialised local season ids; missing entries render null.
// seasonCopies maps season numbers to this user's copy kinds, threaded the same
// way; missing entries render []. seasonStats maps them to this user's derived
// progress and rating average; missing entries render null.
func (s *Service) showDetailsFrom(t provider.TMDBTVDetails, raw json.RawMessage, seasonIDs map[int]int64, seasonCopies map[int][]string, jellyfinEpisodes map[int]int, seasonStats map[int]common.SeasonUserStats, link common.AssetLinker) ShowDetails {
	d := ShowDetails{
		TMDBID:         t.ID,
		Title:          t.Name,
		OriginalTitle:  common.NilIfEmpty(t.OriginalName),
		FirstAirDate:   common.NilIfEmpty(t.FirstAirDate),
		LastAirDate:    common.NilIfEmpty(t.LastAirDate),
		SeasonsCount:   t.SeasonCount,
		EpisodesCount:  t.EpisodeCount,
		ShowStatus:     common.NilIfEmpty(t.Status),
		InProduction:   t.InProduction,
		Overview:       common.NilIfEmpty(t.Overview),
		Tagline:        common.NilIfEmpty(t.Tagline),
		Genres:         []string{},
		CreatedBy:      []string{},
		Networks:       []string{},
		Cast:           []common.CastMember{},
		Seasons:        []ShowSeason{},
		Videos:         []common.MovieVideo{},
		Backdrops:      []common.MovieImage{},
		Posters:        []common.MovieImage{},
		Similar:        []common.SimilarTitle{},
		FetchedAt:      s.Now().UTC().Format(time.RFC3339),
		JellyfinSynced: s.JellyfinSynced(),
	}

	d.Year = common.YearOf(t.FirstAirDate)
	// A returning series has a last_air_date (its most recent episode) but no
	// end year — only a finished show gets one.
	if !t.InProduction {
		d.EndYear = common.YearOf(t.LastAirDate)
	}
	if len(t.EpisodeRunTime) > 0 && t.EpisodeRunTime[0] > 0 {
		r := t.EpisodeRunTime[0]
		d.EpisodeRuntimeMin = &r
	}
	for _, g := range t.Genres {
		if g.Name != "" {
			d.Genres = append(d.Genres, g.Name)
		}
	}
	for _, c := range t.CreatedBy {
		if c.Name != "" {
			d.CreatedBy = append(d.CreatedBy, c.Name)
		}
	}
	for _, n := range t.Networks {
		if n.Name != "" {
			d.Networks = append(d.Networks, n.Name)
		}
	}
	if e := t.NextEpisodeToAir; e != nil {
		d.NextEpisode = &ShowEpisodeStub{
			Season:  e.SeasonNumber,
			Number:  e.EpisodeNumber,
			Title:   common.NilIfEmpty(e.Name),
			AirDate: common.NilIfEmpty(e.AirDate),
		}
	}
	if t.PosterPath != "" {
		thumb := link.Pick(assets.ShowPoster(link.ItemID, true), common.PosterProxyURL(common.PosterSizeThumb, t.PosterPath))
		full := link.Pick(assets.ShowPoster(link.ItemID, false), common.PosterProxyURL(common.PosterSizeFull, t.PosterPath))
		d.Poster, d.PosterFull = &thumb, &full
	}
	if t.BackdropPath != "" {
		thumb := link.Pick(assets.ShowBackdrop(link.ItemID, true), common.PosterProxyURL(common.BackdropSizeThumb, t.BackdropPath))
		full := link.Pick(assets.ShowBackdrop(link.ItemID, false), common.PosterProxyURL(common.BackdropSizeFull, t.BackdropPath))
		d.Backdrop, d.BackdropFull = &thumb, &full
	}
	d.Seasons = showSeasons(t.Seasons, seasonIDs, seasonCopies, jellyfinEpisodes, seasonStats, link)

	// Rating, votes, credits, links and facts live only in the appended
	// sections. Same tolerance as the movie path: an undecodable body renders
	// as "no credits" rather than failing the page.
	var snap common.MediaSnapshot
	_ = json.Unmarshal(raw, &snap)

	d.Streaming = common.StreamingFrom(snap, s.WatchRegion)
	d.Similar = common.SimilarTitles(snap)

	if snap.VoteAverage > 0 {
		v := snap.VoteAverage
		d.Rating = &v
	}
	if snap.VoteCount > 0 {
		n := snap.VoteCount
		d.Votes = &n
	}
	if snap.Popularity > 0 {
		d.Popularity = &snap.Popularity
	}
	d.Homepage = common.NilIfEmpty(snap.Homepage)
	d.IMDBID = common.NilIfEmpty(snap.ExternalIDs.IMDBID)
	d.OriginalLanguage = common.NilIfEmpty(snap.OriginalLanguage)
	d.TrailerURL = common.TrailerURL(snap.Videos.Results)
	d.Cast = showCast(snap, link)
	d.Videos = showVideos(snap.Videos.Results, link)
	d.Backdrops = showImages(snap.Images.Backdrops, common.BackdropSizeThumb, common.BackdropSizeFull, "backdrops", link)
	d.Posters = showImages(snap.Images.Posters, common.PosterSizeThumb, common.PosterSizeFull, "posters", link)
	return d
}

// showSeasons projects TMDB's seasons array into the grid's entries, ordered
// ascending with Specials last: season 0 is not the start of a show, and a grid
// that opens on it reads as a bug.
func showSeasons(seasons []provider.TMDBTVSeason, seasonIDs map[int]int64, seasonCopies map[int][]string, jellyfinEpisodes map[int]int, seasonStats map[int]common.SeasonUserStats, link common.AssetLinker) []ShowSeason {
	ordered := slices.Clone(seasons)
	slices.SortStableFunc(ordered, func(a, b provider.TMDBTVSeason) int {
		if (a.SeasonNumber == 0) != (b.SeasonNumber == 0) {
			if a.SeasonNumber == 0 {
				return 1
			}
			return -1
		}
		return a.SeasonNumber - b.SeasonNumber
	})

	out := make([]ShowSeason, 0, len(ordered))
	for _, s := range ordered {
		season := ShowSeason{
			SeasonNumber: s.SeasonNumber,
			Name:         s.Name,
			EpisodeCount: s.EpisodeCount,
			AirDate:      common.NilIfEmpty(s.AirDate),
			Year:         common.YearOf(s.AirDate),
			Overview:     common.NilIfEmpty(s.Overview),
			Copies:       []string{},
		}
		if id, ok := seasonIDs[s.SeasonNumber]; ok {
			season.ID = &id
		}
		if kinds, ok := seasonCopies[s.SeasonNumber]; ok {
			season.Copies = kinds
		}
		if n, ok := jellyfinEpisodes[s.SeasonNumber]; ok {
			season.JellyfinEpisodes = &n
		}
		// Only a season with episode rows has stats; a snapshot season the
		// catalog has not materialised yet stays null, like an untracked show's.
		if st, ok := seasonStats[s.SeasonNumber]; ok {
			p := st.Progress
			season.Progress = &p
			season.UserRating = st.RatingAvg
		}
		if s.VoteAverage > 0 {
			v := s.VoteAverage
			season.Rating = &v
		}
		if s.PosterPath != "" {
			thumb := link.Pick(assets.ShowSeasonPoster(link.ItemID, s.SeasonNumber, true),
				common.PosterProxyURL(common.PosterSizeThumb, s.PosterPath))
			full := link.Pick(assets.ShowSeasonPoster(link.ItemID, s.SeasonNumber, false),
				common.PosterProxyURL(common.PosterSizeFull, s.PosterPath))
			season.Poster, season.PosterFull = &thumb, &full
		}
		out = append(out, season)
	}
	return out
}

// showCast projects the snapshot's cast into the cast row: ShowCastEntries'
// order, photos resolved through the linker exactly as the movie path does.
func showCast(snap common.MediaSnapshot, link common.AssetLinker) []common.CastMember {
	return castMembers(common.ShowCastEntries(snap), link)
}

// castMembers turns cast entries into the DTO shape, each photo resolved into
// the show's cast directory when the linker knows the file, else the proxy.
func castMembers(entries []common.CastEntry, link common.AssetLinker) []common.CastMember {
	out := make([]common.CastMember, 0, len(entries))
	for _, e := range entries {
		cm := common.CastMember{TMDBID: common.PersonIDOf(e.TMDBID), Name: e.Name, Character: common.NilIfEmpty(e.Character), Episodes: e.Episodes}
		if e.ProfilePath != "" {
			photo := link.Pick(
				assets.ShowCastPhoto(link.ItemID, strings.TrimPrefix(e.ProfilePath, "/")),
				common.PosterProxyURL(common.PosterSizeThumb, e.ProfilePath))
			cm.Photo = &photo
		}
		out = append(out, cm)
	}
	return out
}

// showSnapshotMissingSections is the show half of movieSnapshotMissingSections:
// a stored body that predates the aggregate_credits or recommendations append,
// or carries a provider listing for another country, needs one refresh even
// when it is not yet time-stale. A fresh fetch always writes every key (empty
// or not), so this fires at most once per old row; if the refresh fails the
// serve-stale path applies — ShowCastEntries falls back to the old credits
// section, and the similar rail renders empty.
func showSnapshotMissingSections(metadataJSON, region string) bool {
	var probe struct {
		AggregateCredits json.RawMessage `json:"aggregate_credits"`
		Recommendations  json.RawMessage `json:"recommendations"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &probe); err != nil {
		return true
	}
	return probe.AggregateCredits == nil || probe.Recommendations == nil ||
		watchProvidersStale(metadataJSON, region)
}

// showVideos and showImages are the show halves of movieVideos and movieImages:
// the same ranking helpers, resolved through the same linker. A live show has
// no permanent artwork on disk, so its zero linker proxies everything; a
// tracked show serves what the queue has already landed.
func showVideos(videos []common.SnapshotVideo, link common.AssetLinker) []common.MovieVideo {
	ranked := common.RankedVideos(videos)
	out := make([]common.MovieVideo, 0, len(ranked))
	for _, v := range ranked {
		mv := common.MovieVideo{
			Name:        v.Name,
			Type:        v.Type,
			Official:    v.Official,
			PublishedAt: common.NilIfEmpty(v.PublishedAt),
			URL:         "https://www.youtube.com/watch?v=" + url.QueryEscape(v.Key),
		}
		// A key the proxy would reject gets no thumb rather than a URL that
		// 422s — the SPA renders a placeholder tile for null.
		if common.YouTubeKeyRe.MatchString(v.Key) {
			thumb := link.Pick(assets.ShowVideoThumb(link.ItemID, v.Key),
				"api/v1/video/youtube-thumbs/"+v.Key)
			mv.Thumb = &thumb
		}
		out = append(out, mv)
	}
	return out
}

func showImages(images []common.SnapshotImage, sizeThumb, sizeFull, gallery string, link common.AssetLinker) []common.MovieImage {
	ordered := common.RankedImages(images)
	out := make([]common.MovieImage, 0, len(ordered))
	for _, img := range ordered {
		base := strings.TrimPrefix(img.FilePath, "/")
		out = append(out, common.MovieImage{
			Thumb: link.Pick(assets.ShowGalleryImage(link.ItemID, gallery, base, true),
				common.PosterProxyURL(sizeThumb, img.FilePath)),
			Full: link.Pick(assets.ShowGalleryImage(link.ItemID, gallery, base, false),
				common.PosterProxyURL(sizeFull, img.FilePath)),
			Width:  img.Width,
			Height: img.Height,
		})
	}
	return out
}
