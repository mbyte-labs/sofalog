package details

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// seasonStaleAfter is how old a season's metadata snapshot may get before a
// details view refreshes it from TMDB — the movie window (D-015).
const seasonStaleAfter = 48 * time.Hour

// SeasonDetailsParams carries the raw query strings of GET
// /video/seasons/details; the service owns validation. Either ID alone, or
// TMDBID and Season together — a season's tmdb key is the pair (show tmdb_id,
// season_number), because TMDB's own season ids never appear in a URL here.
type SeasonDetailsParams struct {
	ID     string
	TMDBID string // the SHOW's tmdb id
	Season string // season number; 0 is Specials
}

// SeasonEpisode is one row of the season page's episode list. Nullable fields
// are pointers without omitempty.
type SeasonEpisode struct {
	// ID is the local episodes.id, or null when the show has no catalog row
	// yet — an episode row is created by tracking, or by a season view/refresh
	// of an already-tracked show, never by a view of an untracked one. A null
	// id is not an error: the mark call addresses the episode by the show's
	// tmdb_id plus the two numbers below instead, and tracking mints the id.
	// Exactly the arrangement ShowSeason.ID already uses for the seasons grid.
	ID *int64 `json:"id"`
	// Watched is whether THIS user has ever watched the episode. False on an
	// untracked show, which has no watch rows to find.
	Watched bool `json:"watched"`
	// UserRating is THIS user's rating, 1-10, or null when unrated. A season is
	// not rateable (D-020) but an episode is, so the episode page's bar seeds
	// from here.
	UserRating    *int    `json:"user_rating"`
	EpisodeNumber int     `json:"episode_number"`
	Title         *string `json:"title"`
	Overview      *string `json:"overview"`
	AirDate       *string `json:"air_date"`    // YYYY-MM-DD
	RuntimeMin    *int    `json:"runtime_min"` // null when TMDB has none
	// TMDB's community score for the episode; null when unrated.
	Rating *float64 `json:"rating"`
	Votes  *int64   `json:"votes"`
	// Relative URLs of the locally proxied still variants (w300 for a grid row,
	// w1280 for the episode page's hero), or null when TMDB has none. Never a
	// provider URL.
	Still     *string `json:"still"`
	StillFull *string `json:"still_full"`
	// Marker labels the episodes worth badging, in the up-next cards'
	// vocabulary: season_premiere | mid_season_finale | season_finale, or null
	// for a plain episode. Derived from TMDB's episode_type plus the premiere
	// rule (episode 1); a finale beats a premiere for a one-episode season.
	Marker *string `json:"marker"`
}

// SeasonDetails is the season page's DTO, built from the stored snapshot row —
// or, for a season whose show is untracked, from a live fetch that stored
// nothing.
//
// ID is nullable for exactly the reason MovieDetails' and ShowDetails' are: it
// is null until the season has a catalog row, and only tracking the show
// creates one (D-015). There are no tracking flags — season "watched" is
// derived progress in M2, never a stored boolean, so the eventual fields would
// not be these; adding them later is the sanctioned additive change. Copies and
// ShowDropped are the two per-user fields so far (D-021, D-010).
type SeasonDetails struct {
	ID *int64 `json:"id"`
	// No tmdb_id for the season itself: TMDB's appended season sub-document
	// carries no integer season id, and nothing addresses a season by one —
	// the key is (show_tmdb_id, season_number).
	ShowTMDBID int64 `json:"show_tmdb_id"`
	// ShowTitle is denormalised off the show root at fetch time, so the header
	// and back link work on a direct ?id= visit without a second TMDB call.
	ShowTitle    string  `json:"show_title"`
	SeasonNumber int     `json:"season_number"` // 0 is Specials
	Name         string  `json:"name"`
	Overview     *string `json:"overview"`
	AirDate      *string `json:"air_date"` // YYYY-MM-DD
	Year         *string `json:"year"`     // "2008", derived from AirDate
	EpisodeCount int     `json:"episode_count"`
	// TMDB's community score for the season; null when unrated.
	Rating *float64 `json:"rating"`
	// Relative URLs of the locally proxied poster variants, never a provider
	// URL. Null when TMDB has no season poster.
	Poster     *string         `json:"poster"`
	PosterFull *string         `json:"poster_full"`
	Episodes   []SeasonEpisode `json:"episodes"`
	// Cast is THIS season's aggregate credits — everyone credited in its
	// episodes, ordered by billing, each `episodes` count taken within the
	// season (the show page's counts span the run). Always present; [] when
	// TMDB lists nobody or the snapshot predates the section. Photos resolve
	// through the show's cast directory: a season's cast is a subset of the
	// show's, and the files are keyed by TMDB's filename, so the show's
	// download set already covers them (proxy fallback for any drift between
	// the two snapshots' fetch times).
	Cast      []common.CastMember `json:"cast"`
	FetchedAt string              `json:"fetched_at"` // RFC3339 UTC
	// The kinds this user holds a copy of this season on (D-021) — per-user
	// state, so, like every other axis, [] on a live untracked view. With a
	// Jellyfin server configured the `jellyfin` entry is the scan's (D-033)
	// and JellyfinSynced is true — see MovieDetails.JellyfinSynced.
	Copies         []string `json:"copies"`
	JellyfinSynced bool     `json:"jellyfin_synced"`
	// JellyfinEpisodes is how many of this season's episode files the scan
	// found (D-033), null when unsynced or not on the server — see
	// ShowSeason.JellyfinEpisodes.
	JellyfinEpisodes *int `json:"jellyfin_episodes"`
	// Whether this user has DROPPED the owning show (D-010) — borrowed from the
	// show exactly as Streaming is, and per-user exactly as Copies is. It is
	// here so the page can say "you gave up on this" rather than silently
	// offering the same controls as a show still in progress.
	//
	// A bool rather than the show's status string on purpose: a season-shaped
	// view deliberately reports no show status and no show progress — that is
	// the show page's job — and `dropped` is the one state that changes how the
	// page should read. Not to be confused with ShowDetails.show_status, which
	// is TMDB's PRODUCTION status.
	//
	// False on a live untracked view: no state row exists to be dropped in.
	// Sticky (D-010), so no write from this page can stale it.
	ShowDropped bool `json:"show_dropped"`
	// Where to watch, borrowed from the OWNING SHOW (D-022): TMDB publishes
	// watch providers per movie and per show, never per season, so a season
	// has no listing of its own. Named `streaming` rather than
	// `show_streaming` because it is the show's on every one of the four
	// detail pages — unlike Copies, which really is the season's.
	//
	// NULL while the show is untracked: there is no show row to read it out
	// of, and reaching for TMDB here would be a render-path provider call. The
	// page stays quiet rather than reporting an emptiness it never checked.
	Streaming *common.Streaming `json:"streaming"`
}

// SeasonDetails returns one season, from the local snapshot or live. The
// snapshot contract it obeys is resolveSeasonRow's; this function is that plus
// the DTO projection.
//
// userID is what makes the per-episode mark controls render truthfully. On an
// untracked show both lookups below return empty maps, so every episode comes
// back with a null id and watched=false — which is exactly right: there is
// nothing to have watched yet.
func (s *Service) SeasonDetails(ctx context.Context, userID int64, p SeasonDetailsParams) (SeasonDetails, error) {
	row, err := s.resolveSeasonRow(ctx, p.ID, p.TMDBID, p.Season)
	if err != nil {
		return SeasonDetails{}, err
	}
	link, state, streaming, err := s.seasonRenderState(ctx, userID, row)
	if err != nil {
		return SeasonDetails{}, err
	}
	return seasonDetailsFromRow(row, link, state, streaming, s.JellyfinSynced()), nil
}

// seasonUserState is one user's private state for one season: the three
// per-episode lookups the projection needs, keyed by episode number, plus the
// season's own owned-copy kinds. copies is never nil once seasonRenderState has
// run — the DTOs serve it as [] rather than null.
type seasonUserState struct {
	episodeIDs  map[int]int64
	watched     map[int]bool
	ratings     map[int]int
	copies      []string
	showDropped bool
	// jellyfinEpisodes is the scan's episode-file count for the season, nil
	// when the scan does not own the jellyfin kind or the season is absent.
	jellyfinEpisodes *int
}

// seasonRenderState gathers everything a season projection needs beyond its own
// row: which show subtree its artwork lives in, its episodes' local ids, which
// of them this user has watched, the kinds this user owns the season on,
// whether this user has dropped the owning show, and that show's watch
// providers. Shared by the season and episode views so
// the two cannot render the same season or episode differently — the episode
// page borrows the copies to show them read-only (D-021), since an episode
// never owns one itself, and both borrow the show's providers (D-022), since
// neither has a listing of its own.
//
// The Streaming return is deliberately NOT part of seasonUserState: it is
// catalog data, identical for every user, and folding it in would make a
// per-user struct carry something that is not.
func (s *Service) seasonRenderState(ctx context.Context, userID int64, row common.SeasonRow) (common.AssetLinker, seasonUserState, *common.Streaming, error) {
	// A season of an untracked show was never stored, so it has no show row, no
	// permanent artwork and no episode rows — the zero linker proxies
	// everything and the zero state renders null ids, false flags and no
	// ratings. No show row also means no provider listing to borrow, and
	// fetching one here would be a provider call on a render path.
	//
	// showDropped false is the truth here, not a shortcut: dropping needs a
	// state row, a state row needs an episode watch, and an episode watch needs
	// a tracked show. There is no show id to read one by either.
	if row.ShowID == nil {
		return common.AssetLinker{}, seasonUserState{copies: []string{}}, nil, nil
	}
	var state seasonUserState
	var err error
	if state.episodeIDs, err = common.EpisodeIDsBySeason(ctx, s.DB, row.ShowTMDBID, row.SeasonNumber); err != nil {
		return common.AssetLinker{}, seasonUserState{}, nil, err
	}
	if state.watched, err = common.WatchedEpisodeIDs(ctx, s.DB, userID, row.ShowTMDBID, row.SeasonNumber); err != nil {
		return common.AssetLinker{}, seasonUserState{}, nil, err
	}
	if state.ratings, err = common.RatedEpisodeIDs(ctx, s.DB, userID, row.ShowTMDBID, row.SeasonNumber); err != nil {
		return common.AssetLinker{}, seasonUserState{}, nil, err
	}
	if state.copies, state.jellyfinEpisodes, err = s.CopiesFor(ctx, userID, "season", row.ID); err != nil {
		return common.AssetLinker{}, seasonUserState{}, nil, err
	}
	if state.showDropped, err = common.ShowDroppedFor(ctx, s.DB, userID, *row.ShowID); err != nil {
		return common.AssetLinker{}, seasonUserState{}, nil, err
	}
	// A local read of the show the season already links to — the season's own
	// snapshot is the season sub-document, which carries no providers.
	show, err := common.GetShowByID(ctx, s.DB, *row.ShowID)
	if err != nil {
		return common.AssetLinker{}, seasonUserState{}, nil, err
	}
	var snap common.MediaSnapshot
	_ = json.Unmarshal([]byte(show.MetadataJSON), &snap)
	return common.ShowLinker(*row.ShowID, s.ShowFiles(*row.ShowID)), state, common.StreamingFrom(snap, s.WatchRegion), nil
}

// resolveSeasonRow turns the addressing parameters of a season-shaped request
// into a snapshot row, per D-015 — the same three branches the movie and show
// views have, with no season exception any more:
//
//   - tmdb-key lookup with no local row, show UNTRACKED: fetch the season and
//     serve it live, storing nothing. TMDB errors propagate as a 503; with no
//     row there is nothing to fall back on.
//   - tmdb-key lookup with no local row, show TRACKED: materialise and link the
//     season, exactly as RefreshShow's new-season branch would. The show is
//     already in the catalog, so this creates nothing a user did not ask for —
//     and without it the season's episodes would have no rows and could never
//     be marked.
//   - any lookup with a row: serve the snapshot; one older than
//     seasonStaleAfter — or predating an appended section
//     (seasonSnapshotMissingSections) — is refreshed synchronously, and if that
//     refresh fails the stale snapshot is served and the failure logged — a
//     season with a row never turns into a 503.
//   - id lookup with no row: a plain 404, never a TMDB call.
//
// A seasons row therefore exists if and only if its show is tracked, which is
// what lets seasons.show_id be NOT NULL.
//
// Both the season details view and the episode details view address a season
// exactly this way, so the contract lives here once. The parameters are the
// raw query strings; validation is this layer's job.
func (s *Service) resolveSeasonRow(ctx context.Context, id, tmdbID, season string) (common.SeasonRow, error) {
	byID := id != ""
	byKey := tmdbID != "" || season != ""
	if byID == byKey {
		return common.SeasonRow{}, fmt.Errorf("%w: either id, or tmdb_id and season together, is required", httpx.ErrValidation)
	}

	var row common.SeasonRow
	if byID {
		seasonID, err := strconv.ParseInt(id, 10, 64)
		if err != nil || seasonID < 1 {
			return common.SeasonRow{}, fmt.Errorf("%w: id must be a positive integer", httpx.ErrValidation)
		}
		row, err = common.GetSeasonByID(ctx, s.DB, seasonID)
		if err != nil {
			return common.SeasonRow{}, err
		}
	} else {
		if tmdbID == "" || season == "" {
			return common.SeasonRow{}, fmt.Errorf("%w: tmdb_id and season are required together", httpx.ErrValidation)
		}
		showTMDBID, err := strconv.ParseInt(tmdbID, 10, 64)
		if err != nil || showTMDBID < 1 {
			return common.SeasonRow{}, fmt.Errorf("%w: tmdb_id must be a positive integer", httpx.ErrValidation)
		}
		// 0 is a real season — Specials — so only negatives are invalid.
		seasonNumber, err := strconv.Atoi(season)
		if err != nil || seasonNumber < 0 {
			return common.SeasonRow{}, fmt.Errorf("%w: season must be a non-negative integer", httpx.ErrValidation)
		}
		row, err = common.GetSeasonByTMDBKey(ctx, s.DB, showTMDBID, seasonNumber)
		if errors.Is(err, httpx.ErrNotFound) {
			show, showErr := common.GetShowByTMDBID(ctx, s.DB, showTMDBID)
			if errors.Is(showErr, httpx.ErrNotFound) {
				// Untracked: live, and storing nothing (D-015). The row struct
				// is just the in-memory carrier; ID stays 0 (= no row) and
				// ShowID stays nil, so seasonRenderState renders the zero state.
				return s.FetchSeason(ctx, showTMDBID, seasonNumber)
			}
			if showErr != nil {
				return common.SeasonRow{}, showErr
			}
			// Tracked, but TMDB has added a season since the show was written.
			// Materialise and link it, episodes and all.
			return s.RefreshSeason(ctx, show, seasonNumber)
		}
		if err != nil {
			return common.SeasonRow{}, err
		}
	}

	if s.seasonSnapshotStale(row.FetchedAt) || seasonSnapshotMissingSections(row.MetadataJSON) {
		// A row implies a tracked show, so the link is always resolvable.
		show, err := common.GetShowByID(ctx, s.DB, *row.ShowID)
		if err != nil {
			return common.SeasonRow{}, err
		}
		fresh, err := s.RefreshSeason(ctx, show, row.SeasonNumber)
		if err != nil {
			// Serving slightly old data beats failing a page the library can
			// answer — the local-first contract (D-015).
			slog.Warn("tmdb: season refresh failed, serving stale snapshot",
				"season_id", row.ID, "show_tmdb_id", row.ShowTMDBID,
				"season_number", row.SeasonNumber, "fetched_at", row.FetchedAt, "err", err)
		} else {
			row = fresh
		}
	}
	return row, nil
}

// seasonSnapshotStale mirrors movieSnapshotStale: an unparseable fetched_at
// counts as stale, so refreshing rewrites it — the self-repair path.
func (s *Service) seasonSnapshotStale(fetchedAt string) bool {
	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return true
	}
	return s.Now().Sub(t) > seasonStaleAfter
}

// seasonSnapshotMissingSections is the season half of
// showSnapshotMissingSections: a stored season document written before the
// season's aggregate credits (the season page's cast) or its regular credits
// (the episode page's cast) were appended lacks that key entirely and needs one
// refresh even when it is not yet time-stale. TVSeasonDetails always writes
// both keys (null when TMDB sent no section), so this fires at most once per
// old row per widening; if the refresh fails the serve-stale path applies and
// the missing cast renders empty.
func seasonSnapshotMissingSections(metadataJSON string) bool {
	var probe struct {
		AggregateCredits json.RawMessage `json:"aggregate_credits"`
		Credits          json.RawMessage `json:"credits"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &probe); err != nil {
		return true
	}
	return probe.AggregateCredits == nil || probe.Credits == nil
}

// seasonDetailsFromRow builds the DTO from a stored row. Same tolerance as the
// movie path: an undecodable snapshot renders as "no episodes" rather than
// failing the page.
func seasonDetailsFromRow(row common.SeasonRow, link common.AssetLinker, state seasonUserState, streaming *common.Streaming, jellyfinSynced bool) SeasonDetails {
	d := SeasonDetails{
		ShowTMDBID:   row.ShowTMDBID,
		ShowTitle:    row.ShowTitle,
		SeasonNumber: row.SeasonNumber,
		Name:         row.Name,
		Overview:     row.Overview,
		AirDate:      row.AirDate,
		EpisodeCount: row.EpisodeCount,
		Episodes:     []SeasonEpisode{},
		Cast:         []common.CastMember{},
		FetchedAt:    row.FetchedAt,
		Copies:       state.copies,
		ShowDropped:  state.showDropped,
		Streaming:    streaming,
	}
	d.JellyfinSynced = jellyfinSynced
	d.JellyfinEpisodes = state.jellyfinEpisodes
	if row.ID > 0 {
		// 0 means "no catalog row" — an untracked show's season renders id null.
		id := row.ID
		d.ID = &id
	}
	if row.AirDate != nil {
		d.Year = common.YearOf(*row.AirDate)
	}
	if row.PosterPath != nil {
		thumb := link.Pick(assets.ShowSeasonPoster(link.ItemID, row.SeasonNumber, true),
			common.PosterProxyURL(common.PosterSizeThumb, *row.PosterPath))
		full := link.Pick(assets.ShowSeasonPoster(link.ItemID, row.SeasonNumber, false),
			common.PosterProxyURL(common.PosterSizeFull, *row.PosterPath))
		d.Poster, d.PosterFull = &thumb, &full
	}

	var snap common.SeasonSnapshot
	_ = json.Unmarshal([]byte(row.MetadataJSON), &snap)

	// The season's own aggregate credits, through the show's projection: same
	// billing order, same role join, photos linked into the show's cast
	// directory (link.ItemID is the show's id, as for every season asset).
	var media common.MediaSnapshot
	_ = json.Unmarshal([]byte(row.MetadataJSON), &media)
	d.Cast = showCast(media, link)

	if snap.VoteAverage > 0 {
		v := snap.VoteAverage
		d.Rating = &v
	}
	for _, e := range snap.Episodes {
		ep := SeasonEpisode{
			EpisodeNumber: e.EpisodeNumber,
			Title:         common.NilIfEmpty(e.Name),
			Overview:      common.NilIfEmpty(e.Overview),
			AirDate:       common.NilIfEmpty(e.AirDate),
		}
		// TMDB reports 0 for an unknown runtime and for an unrated episode —
		// null is the honest rendering of both.
		if e.Runtime > 0 {
			r := e.Runtime
			ep.RuntimeMin = &r
		}
		if e.VoteAverage > 0 {
			v := e.VoteAverage
			ep.Rating = &v
		}
		if e.VoteCount > 0 {
			n := e.VoteCount
			ep.Votes = &n
		}
		if e.StillPath != "" {
			still := link.Pick(assets.ShowEpisodeStill(link.ItemID, row.SeasonNumber, e.EpisodeNumber, true),
				common.PosterProxyURL(common.StillSizeSingle, e.StillPath))
			full := link.Pick(assets.ShowEpisodeStill(link.ItemID, row.SeasonNumber, e.EpisodeNumber, false),
				common.PosterProxyURL(common.StillSizeFull, e.StillPath))
			ep.Still, ep.StillFull = &still, &full
		}
		if id, ok := state.episodeIDs[e.EpisodeNumber]; ok {
			ep.ID = &id
			ep.Watched = state.watched[e.EpisodeNumber]
			if r, rated := state.ratings[e.EpisodeNumber]; rated {
				ep.UserRating = &r
			}
		}
		ep.Marker = common.EpisodeMarker(e)
		d.Episodes = append(d.Episodes, ep)
	}
	return d
}
