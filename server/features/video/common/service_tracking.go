package common

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"mbyte.ro/sofalog/httpx"
)

// TrackParams carries the polymorphic body of POST /video/watches and
// POST /video/watchlist. Exactly one of TMDBID and TargetID must be set; the
// pair (TargetType, id) is what identifies a target — never an id alone
// (D-013).
type TrackParams struct {
	TargetType string `json:"target_type"` // movie | show | episode
	// For a movie, the movie's TMDB id. For a show OR an episode, the SHOW's:
	// episodes have no TMDB id of their own here, because they ride their
	// season's document rather than being addressed directly.
	TMDBID *int64 `json:"tmdb_id"`
	// Local catalog id in the target's OWN table (movies.id | shows.id |
	// episodes.id). Ids are unique only within their table (D-013), which is
	// exactly why this is meaningless without target_type.
	TargetID *int64 `json:"target_id"`
	// Episode targets only, and only alongside tmdb_id: an episode addressed
	// before its show is tracked has no local id yet, and the two positional
	// numbers are the only durable handle its page holds.
	Season  *int `json:"season"`
	Episode *int `json:"episode"`
}

var (
	// NOT 'season' (D-020). The refusal lives here and nowhere else.
	RateableTargets = []string{"movie", "show", "episode"}
)

var (
	// The watchlist's twin (D-030): a favorite names a title, not a part of
	// one, so seasons and episodes are refused for the same reason.
	FavoriteTargets = []string{"movie", "show"}
)

// assertAllowedTarget is the one membership check. The message is generated
// from the allow-list, so it can never disagree with the policy above.
func assertAllowedTarget(targetType string, allowed []string) error {
	if slices.Contains(allowed, targetType) {
		return nil
	}
	return fmt.Errorf("%w: target_type must be one of %s", httpx.ErrValidation,
		strings.Join(allowed, ", "))
}

// TrackShow is the owning show's state after a tracking call that touched it —
// null for a movie target. An episode mark changes both the show's status and
// its derived progress, and nothing else in the response would tell the page
// that, so it travels back rather than costing a second request.
type TrackShow struct {
	ID       int64              `json:"id"`
	Status   *string            `json:"status"` // watching | watched | dropped
	Progress ShowProgressCounts `json:"progress"`
}

// TrackResult reports what a tracking call did. TargetID is the local catalog
// id — for a target tracked from a live view this is the id the materialised
// row just received, which the SPA adopts in place of its null one.
type TrackResult struct {
	Created    bool   `json:"created"`
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	// Null for a movie, so the movie contract is byte-identical to before.
	Show *TrackShow `json:"show"`
}

// TrackedTarget is what a tracking call resolved to: the local row, plus which
// asset subtree the caller must reconcile afterwards.
type TrackedTarget struct {
	TargetType string
	TargetID   int64 // movies.id | shows.id | seasons.id | episodes.id
	movie      *MovieRow
	Show       *ShowRow // set for show, season AND episode targets
	Season     *SeasonRow
	episode    *episodeRow
}

// assertOneAddress enforces the xor every addressing form shares.
func assertOneAddress(p TrackParams) error {
	if (p.TMDBID == nil) == (p.TargetID == nil) {
		return fmt.Errorf("%w: exactly one of tmdb_id and target_id is required", httpx.ErrValidation)
	}
	if p.TargetID != nil && *p.TargetID < 1 {
		return fmt.Errorf("%w: target_id must be a positive integer", httpx.ErrValidation)
	}
	if p.TMDBID != nil && *p.TMDBID < 1 {
		return fmt.Errorf("%w: tmdb_id must be a positive integer", httpx.ErrValidation)
	}
	return nil
}

// ResolveTrackedTarget maps TrackParams to the catalog row they target,
// materialising it from TMDB when a tmdb_id has no local row yet — tracking is
// what creates catalog rows (D-015), and this is the add-item enrichment call
// site. A target_id lookup never touches TMDB; a miss is a plain 404. With TMDB
// down a materialisation fails with ErrProviderUnavailable and nothing is
// written.
func (s *Core) ResolveTrackedTarget(ctx context.Context, p TrackParams, allowed []string) (TrackedTarget, error) {
	if err := assertAllowedTarget(p.TargetType, allowed); err != nil {
		return TrackedTarget{}, err
	}
	if err := assertOneAddress(p); err != nil {
		return TrackedTarget{}, err
	}
	// The positional fields ride only the targets they can address: episode
	// takes both, season takes its own number, everything else takes neither.
	if p.Episode != nil && p.TargetType != "episode" {
		return TrackedTarget{}, fmt.Errorf(
			"%w: episode belongs to an episode target", httpx.ErrValidation)
	}
	if p.Season != nil && p.TargetType != "episode" && p.TargetType != "season" {
		return TrackedTarget{}, fmt.Errorf(
			"%w: season belongs to an episode or season target", httpx.ErrValidation)
	}

	switch p.TargetType {
	case "movie":
		row, err := s.resolveTrackedMovie(ctx, p)
		if err != nil {
			return TrackedTarget{}, err
		}
		return TrackedTarget{TargetType: "movie", TargetID: row.ID, movie: &row}, nil
	case "show":
		row, err := s.resolveTrackedShowTarget(ctx, p)
		if err != nil {
			return TrackedTarget{}, err
		}
		return TrackedTarget{TargetType: "show", TargetID: row.ID, Show: &row}, nil
	case "season":
		return s.resolveTrackedSeason(ctx, p)
	case "episode":
		return s.resolveTrackedEpisode(ctx, p)
	}
	// Unreachable: assertAllowedTarget already rejected anything else.
	return TrackedTarget{}, fmt.Errorf("%w: unsupported target_type", httpx.ErrValidation)
}

// resolveTrackedMovie maps a movie-shaped TrackParams to its movies row.
func (s *Core) resolveTrackedMovie(ctx context.Context, p TrackParams) (MovieRow, error) {
	if p.TargetID != nil {
		return GetMovieByID(ctx, s.DB, *p.TargetID)
	}
	row, err := GetMovieByTMDBID(ctx, s.DB, *p.TMDBID)
	if errors.Is(err, httpx.ErrNotFound) {
		return s.RefreshMovie(ctx, *p.TMDBID)
	}
	return row, err
}

// resolveTrackedShowTarget maps a show-shaped TrackParams to its shows row,
// materialising the whole show — every season and episode — on a tmdb_id miss.
func (s *Core) resolveTrackedShowTarget(ctx context.Context, p TrackParams) (ShowRow, error) {
	if p.TargetID != nil {
		return GetShowByID(ctx, s.DB, *p.TargetID)
	}
	return s.ResolveTrackedShow(ctx, *p.TMDBID)
}

// resolveTrackedSeason maps a season-shaped TrackParams to its seasons row —
// the target the bulk mark's season scope names. Two addressing forms:
//
//   - target_id alone is seasons.id, and like every other target_id path it
//     never calls TMDB. A season row only ever exists beneath a TRACKED show
//     (D-015), so its show_id always resolves locally, and the episode rows the
//     bulk insert selects were written in the same transaction as the season.
//   - tmdb_id + season, where tmdb_id is the SHOW's — the same primary form the
//     episode arm uses, for the same reason, and the one the season page uses
//     when the show is not tracked yet. ResolveTrackedShow materialises the
//     show, its seasons and its episodes, so the season row exists by the time
//     it is looked up below.
//
// A season number the show does not have is a 404, not a materialisation.
func (s *Core) resolveTrackedSeason(ctx context.Context, p TrackParams) (TrackedTarget, error) {
	if p.TargetID != nil {
		if p.Season != nil {
			return TrackedTarget{}, fmt.Errorf(
				"%w: target_id already identifies the season; drop season", httpx.ErrValidation)
		}
		row, err := GetSeasonByID(ctx, s.DB, *p.TargetID)
		if err != nil {
			return TrackedTarget{}, err
		}
		show, err := GetShowByID(ctx, s.DB, *row.ShowID)
		if err != nil {
			return TrackedTarget{}, err
		}
		return TrackedTarget{TargetType: "season", TargetID: row.ID, Show: &show, Season: &row}, nil
	}

	if p.Season == nil {
		return TrackedTarget{}, fmt.Errorf(
			"%w: a season addressed by tmdb_id needs season too", httpx.ErrValidation)
	}
	// 0 is a real season — Specials — so only negatives are invalid here; the
	// bulk mark refuses Specials itself, with a message that says why.
	if *p.Season < 0 {
		return TrackedTarget{}, fmt.Errorf(
			"%w: season must be a non-negative integer", httpx.ErrValidation)
	}

	show, err := s.ResolveTrackedShow(ctx, *p.TMDBID)
	if err != nil {
		return TrackedTarget{}, err
	}
	row, err := GetSeasonByTMDBKey(ctx, s.DB, show.TMDBID, *p.Season)
	if err != nil {
		return TrackedTarget{}, err
	}
	return TrackedTarget{TargetType: "season", TargetID: row.ID, Show: &show, Season: &row}, nil
}

// resolveTrackedEpisode maps an episode-shaped TrackParams to its episodes row,
// in either of two addressing forms:
//
//   - target_id alone is episodes.id — the fast path once the show is tracked
//     and the client has read the id off the season or episode DTO. Never a
//     TMDB call; a miss is a plain 404.
//   - tmdb_id + season + episode, where tmdb_id is the SHOW's. This is the
//     PRIMARY form, because those three are the only handles the episode page
//     holds before its show is tracked — and resolving it materialises the
//     show, which is what makes marking one episode pull the whole show local.
//
// An episode number the show does not have is a 404, not a materialisation.
func (s *Core) resolveTrackedEpisode(ctx context.Context, p TrackParams) (TrackedTarget, error) {
	if p.TargetID != nil {
		if p.Season != nil || p.Episode != nil {
			return TrackedTarget{}, fmt.Errorf(
				"%w: target_id already identifies the episode; drop season and episode", httpx.ErrValidation)
		}
		ep, err := getEpisodeByID(ctx, s.DB, *p.TargetID)
		if err != nil {
			return TrackedTarget{}, err
		}
		show, err := GetShowByID(ctx, s.DB, ep.ShowID)
		if err != nil {
			return TrackedTarget{}, err
		}
		return TrackedTarget{TargetType: "episode", TargetID: ep.ID, Show: &show, episode: &ep}, nil
	}

	if p.Season == nil || p.Episode == nil {
		return TrackedTarget{}, fmt.Errorf(
			"%w: an episode addressed by tmdb_id needs season and episode too", httpx.ErrValidation)
	}
	// 0 is valid for both: season 0 is Specials, and a recap or pilot may be
	// numbered 0. Only negatives are invalid.
	if *p.Season < 0 || *p.Episode < 0 {
		return TrackedTarget{}, fmt.Errorf(
			"%w: season and episode must be non-negative integers", httpx.ErrValidation)
	}

	show, err := s.ResolveTrackedShow(ctx, *p.TMDBID)
	if err != nil {
		return TrackedTarget{}, err
	}
	ep, err := GetEpisodeByShowKey(ctx, s.DB, show.ID, *p.Season, *p.Episode)
	if err != nil {
		return TrackedTarget{}, err
	}
	return TrackedTarget{TargetType: "episode", TargetID: ep.ID, Show: &show, episode: &ep}, nil
}

// SyncTargetAssets reconciles whichever subtree the resolved target belongs to.
// An episode's artwork lives under its show's directory, so an episode mark
// syncs the show — that is the whole point of the one-unit arrangement.
func (s *Core) SyncTargetAssets(ctx context.Context, t TrackedTarget) {
	switch {
	case t.movie != nil:
		s.SyncMovieAssets(ctx, *t.movie)
	case t.Show != nil:
		s.SyncShowAssets(ctx, *t.Show)
	}
}

// The two ways a client-supplied instant can be refused, kept apart because
// the backup restore reports them as different skip reasons.
var (
	ErrInstantUnparsable = errors.New("instant is not an RFC3339 timestamp")
	ErrInstantFuture     = errors.New("instant is in the future")
)

// NormaliseInstant is the one rule for storing a caller's timestamp: parse it
// as RFC3339 (fractional seconds accepted), convert to UTC, refuse the future,
// and re-format without millis or offset so the stored string sorts. There is
// no lower bound — a watch really can predate the library.
func NormaliseInstant(raw string, now time.Time) (string, error) {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", ErrInstantUnparsable
	}
	t = t.UTC()
	if t.After(now) {
		return "", ErrInstantFuture
	}
	return t.Format(time.RFC3339), nil
}

// ParsePathTarget validates the (target_type, target_id) pair a DELETE carries
// in its path against one operation's allow-list — the string-path counterpart
// of ResolveTrackedTarget's first two checks, sharing the same policy vars.
func ParsePathTarget(targetType, targetID string, allowed []string) (int64, error) {
	if err := assertAllowedTarget(targetType, allowed); err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(targetID, 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%w: target_id must be a positive integer", httpx.ErrValidation)
	}
	return id, nil
}
