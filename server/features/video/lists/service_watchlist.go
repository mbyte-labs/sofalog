package lists

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

var (
	// Movies AND shows: the list projects both arms, so a listed show is
	// visible rather than written-and-invisible. NOT 'episode' — you watchlist
	// a thing you mean to start, and an episode is not that. A watchlist row on
	// its own is a D-008 reference, so watchlisting keeps the catalog row and
	// its artwork alive with no watch and no rating.
	watchlistTargets = []string{"movie", "show"}
)

// WatchlistAdd puts a movie or show on this user's watchlist — an independent
// axis, not a status (D-011). Adding an already-listed target is a no-op
// reported as created=false. For a user with a working TMDB link the addition
// is also queued for their TMDB watchlist, in the same transaction (D-029);
// the enqueue decides that in its SQL, so there is no branch here.
func (s *Service) WatchlistAdd(ctx context.Context, userID int64, p common.TrackParams) (common.TrackResult, error) {
	target, err := s.ResolveTrackedTarget(ctx, p, watchlistTargets)
	if err != nil {
		return common.TrackResult{}, err
	}

	now := s.Now().UTC().Format(time.RFC3339)
	var created bool
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		var txErr error
		created, txErr = common.InsertWatchlist(ctx, tx, userID, target.TargetType, target.TargetID, now)
		if txErr != nil {
			return txErr
		}
		return common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindWatchlist, target.TargetType, target.TargetID, "set", nil, now)
	})
	if err != nil {
		return common.TrackResult{}, err
	}
	s.KickTMDBSync()
	s.SyncTargetAssets(ctx, target)
	return common.TrackResult{Created: created, TargetType: target.TargetType, TargetID: target.TargetID}, nil
}

// WatchlistRemove takes a target off this user's watchlist. Idempotent —
// removing what is not listed is a success — and never a provider call on
// this request: for a linked user the removal is queued for their TMDB
// watchlist (D-029). The catalog row and its assets stay: untracking never
// hard-deletes (D-008); the deferred sweep collects unreferenced rows once it
// lands.
func (s *Service) WatchlistRemove(ctx context.Context, userID int64, targetType, targetID string) error {
	id, err := common.ParsePathTarget(targetType, targetID, watchlistTargets)
	if err != nil {
		return err
	}
	now := s.Now().UTC().Format(time.RFC3339)
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if err := deleteWatchlist(ctx, tx, userID, targetType, id); err != nil {
			return err
		}
		return common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindWatchlist, targetType, id, "delete", nil, now)
	})
	if err != nil {
		return err
	}
	s.KickTMDBSync()
	return nil
}

// WatchlistEntry is one card on the watchlist page: one row of
// user_video_watchlist, with the item it names projected alongside.
//
// The watchlist is an INDEPENDENT axis, not a status (D-011) — Watched is
// therefore an ordinary field and not a contradiction, because a film can be
// listed and already seen at once. The list is one row per target (the table's
// primary key says so), so unlike the history feed the same item never appears
// twice and the target's id is a stable list key.
//
// Movie and Show form a discriminated union keyed by TargetType: exactly one is
// non-nil. The pair (TargetType, that object's ID) identifies the target; there
// is deliberately no bare target_id in the payload, because an id alone
// identifies nothing — movie 5 and episode 5 both exist (D-013).
//
// Nullable fields are pointers without omitempty — the SPA distinguishes null
// from absent.
type WatchlistEntry struct {
	// The instant the item was put on the list, RFC3339 UTC. Clients render it
	// in the viewer's local time; the server never emits an offset.
	AddedAt    string `json:"added_at"`
	TargetType string `json:"target_type"` // movie | show
	// Relative asset paths, or null together. The grid-sized poster (w185)
	// and the same poster at the full size (w500): the card offers both
	// through srcset and the browser picks by screen density.
	Poster     *string `json:"poster"`
	PosterFull *string `json:"poster_full"`
	// TMDB community score (0-10) from the stored metadata snapshot, or null
	// when the snapshot has none. A provider value and a float — a different
	// thing from UserRating below.
	Rating *float64 `json:"rating"`
	// THIS user's rating of the target, 1-10, or null when unrated.
	UserRating *int `json:"user_rating"`
	// Whether this user has watched the target. Both axes at once is the
	// feature, not an inconsistency to normalise away (D-011).
	//
	// For a movie: any watch in the log. For a show: COMPLETED — the stored
	// status has reached 'watched', meaning every aired, non-special episode
	// (D-012). "Some of it" is not what a ✓ on a show card can honestly mean,
	// and the show's own page carries the partial figure.
	Watched bool            `json:"watched"`
	Movie   *WatchlistMovie `json:"movie"` // non-nil iff target_type == "movie"
	Show    *WatchlistShow  `json:"show"`  // non-nil iff target_type == "show"
}

type WatchlistMovie struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	ReleaseDate *string `json:"release_date"` // YYYY-MM-DD, or null when undated
}

// WatchlistShow is the show arm of the union. FirstAirDate is a show's release
// date — it is what order_by=release_date sorts a show on, and what a card
// captions with a year.
type WatchlistShow struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	FirstAirDate *string `json:"first_air_date"` // YYYY-MM-DD, or null when undated
}

// WatchlistParams carries the query parameters as received; Watchlist defaults
// and validates them so the handler stays free of domain branching.
type WatchlistParams struct {
	OrderBy string // "" | added | title | release_date
	Order   string // "" | asc | desc
	Type    string // "" | all | movie | show
	// Limit and Offset slice the list after ordering. The response stays a
	// bare array; a client asking for limit N knows more may exist exactly
	// when it received N entries back.
	Limit  string // positive integer; "" = unlimited
	Offset string // non-negative integer; "" = 0
	// MatchUsers is the repeated `match_user` query parameter: ids of other
	// accounts whose watchlists this one is intersected with (D-028). Every
	// entry returned is on the caller's list AND on each of theirs.
	MatchUsers []string
}

// maxMatchUsers caps one intersection. A household has a handful of accounts;
// the cap bounds the query's EXISTS chain, not a real use.
const maxMatchUsers = 10

// MatchUser is one account the caller may intersect their watchlist with
// (D-028): the id `match_user` takes and the label the picker shows. No
// email — the label already applied the D-019 fallback server-side.
type MatchUser struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
}

// WatchlistMatchUsers lists every OTHER account that has turned "Allow
// matching watchlist" on, ordered by label. A user who has not opted in is
// not here and cannot be named in `match_user` either.
func (s *Service) WatchlistMatchUsers(ctx context.Context, userID int64) ([]MatchUser, error) {
	rows, err := listMatchUsers(ctx, s.DB, userID)
	if err != nil {
		return nil, err
	}
	// The empty literal keeps the contract a bare array — never encode null.
	out := make([]MatchUser, 0, len(rows))
	for _, r := range rows {
		out = append(out, MatchUser{ID: r.ID, Label: r.Label})
	}
	return out, nil
}

// resolveMatchUsers validates the repeated match_user parameter: positive
// integers, not the caller, no duplicates, at most maxMatchUsers, and every one
// an account that has opted in. That last check is what turns "they switched
// it off since you picked them" into a 422 the client can act on, instead of
// a silently empty list that reads as "nothing in common".
func (s *Service) resolveMatchUsers(ctx context.Context, userID int64, raw []string) ([]int64, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > maxMatchUsers {
		return nil, fmt.Errorf("%w: at most %d match_user values", httpx.ErrValidation, maxMatchUsers)
	}
	ids := make([]int64, 0, len(raw))
	for _, v := range raw {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%w: match_user must be a positive integer", httpx.ErrValidation)
		}
		if n == userID {
			return nil, fmt.Errorf("%w: match_user cannot name yourself", httpx.ErrValidation)
		}
		if slices.Contains(ids, n) {
			return nil, fmt.Errorf("%w: match_user %d is repeated", httpx.ErrValidation, n)
		}
		ids = append(ids, n)
	}
	sharing, err := listMatchUsers(ctx, s.DB, userID)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if !slices.ContainsFunc(sharing, func(u matchUser) bool { return u.ID == id }) {
			return nil, fmt.Errorf("%w: user %d is not sharing their watchlist", httpx.ErrValidation, id)
		}
	}
	return ids, nil
}

// listPage is the defaulted, validated form of the query parameters every
// interleaved movie-and-show list takes — the watchlist's and the favorites'
// (D-030). One parser, so the two lists cannot drift on what "order_by=title"
// or an empty limit means.
type listPage struct {
	OrderBy string // added | title | release_date
	Type    string // all | movie | show
	Desc    bool
	Limit   int // -1 = unlimited
	Offset  int
}

// parseListPage applies the defaults (added, desc, all, unlimited, 0) and
// refuses anything outside the vocabulary with ErrValidation.
func parseListPage(orderBy, order, targetType, limit, offset string) (listPage, error) {
	if orderBy == "" {
		orderBy = "added"
	}
	switch orderBy {
	case "added", "title", "release_date":
	default:
		return listPage{}, fmt.Errorf("%w: order_by must be one of added, title, release_date", httpx.ErrValidation)
	}

	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return listPage{}, fmt.Errorf("%w: order must be asc or desc", httpx.ErrValidation)
	}

	if targetType == "" {
		targetType = "all"
	}
	switch targetType {
	case "all", "movie", "show":
	default:
		return listPage{}, fmt.Errorf("%w: type must be one of all, movie, show", httpx.ErrValidation)
	}

	out := listPage{OrderBy: orderBy, Type: targetType, Desc: order == "desc", Limit: -1}
	if limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 1 {
			return listPage{}, fmt.Errorf("%w: limit must be a positive integer", httpx.ErrValidation)
		}
		out.Limit = n
	}
	if offset != "" {
		n, err := strconv.Atoi(offset)
		if err != nil || n < 0 {
			return listPage{}, fmt.Errorf("%w: offset must be a non-negative integer", httpx.ErrValidation)
		}
		out.Offset = n
	}
	return out, nil
}

func (s *Service) Watchlist(ctx context.Context, userID int64, p WatchlistParams) ([]WatchlistEntry, error) {
	page, err := parseListPage(p.OrderBy, p.Order, p.Type, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}

	matchUsers, err := s.resolveMatchUsers(ctx, userID, p.MatchUsers)
	if err != nil {
		return nil, err
	}

	targets, err := listWatchlistTargets(ctx, s.DB, watchlistFilter{
		UserID:     userID,
		OrderBy:    page.OrderBy,
		Type:       page.Type,
		Desc:       page.Desc,
		Limit:      page.Limit,
		Offset:     page.Offset,
		MatchUsers: matchUsers,
	})
	if err != nil {
		return nil, err
	}
	return s.projectListEntries(ctx, userID, targets)
}

// projectListEntries turns one ordered page of targets into cards, one read
// per entry, for the watchlist and the favorites alike.
func (s *Service) projectListEntries(ctx context.Context, userID int64, targets []watchlistTarget) ([]WatchlistEntry, error) {
	// The empty literal keeps the contract a bare array — never encode null.
	entries := make([]WatchlistEntry, 0, len(targets))
	for _, t := range targets {
		var entry WatchlistEntry
		var err error
		if t.TargetType == "show" {
			entry, err = s.watchlistShowEntry(ctx, userID, t)
		} else {
			entry, err = s.watchlistMovieEntry(ctx, userID, t)
		}
		// The query already dropped a dangling target, but it is one statement
		// ago: a catalog row deleted in between is one entry less, not a failed
		// page. No per-target memoisation like the history feed's either — the
		// list cannot repeat a target.
		if errors.Is(err, httpx.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// watchlistMovieEntry projects one watchlisted movie. It goes through the same
// details projection the movie page uses, so poster choice and community rating
// have a single source and the list renders identically with TMDB down.
func (s *Service) watchlistMovieEntry(ctx context.Context, userID int64, t watchlistTarget) (WatchlistEntry, error) {
	row, err := common.GetMovieByID(ctx, s.DB, t.TargetID)
	if err != nil {
		return WatchlistEntry{}, err
	}
	state, err := common.UserMovieFlags(ctx, s.DB, userID, row.ID)
	if err != nil {
		return WatchlistEntry{}, err
	}
	details := common.MovieDetailsFromRow(row, s.MovieFiles(row.ID), s.WatchRegion)
	return WatchlistEntry{
		AddedAt:    t.AddedAt,
		TargetType: "movie",
		Poster:     details.Poster,
		PosterFull: details.PosterFull,
		Rating:     details.Rating,
		UserRating: state.Rating,
		Watched:    state.Watched,
		Movie: &WatchlistMovie{
			ID:          row.ID,
			Title:       row.Title,
			ReleaseDate: row.ReleaseDate,
		},
		// Show stays nil and so serializes as an explicit null.
	}, nil
}

// watchlistShowEntry projects one watchlisted show. Deliberately NOT through
// showDetailsFrom: that parses cast, videos and images and runs a season query
// per show, and a card needs a poster, a score and a name. Both come out the way
// the history feed's episode entries get theirs — through the show's asset
// linker, so a tracked show's card renders from disk with TMDB down.
func (s *Service) watchlistShowEntry(ctx context.Context, userID int64, t watchlistTarget) (WatchlistEntry, error) {
	row, err := common.GetShowByID(ctx, s.DB, t.TargetID)
	if err != nil {
		return WatchlistEntry{}, err
	}
	state, err := common.UserShowFlags(ctx, s.DB, userID, row.ID)
	if err != nil {
		return WatchlistEntry{}, err
	}
	e := WatchlistEntry{
		AddedAt:    t.AddedAt,
		TargetType: "show",
		UserRating: state.Rating,
		Watched:    state.Status != nil && *state.Status == "watched",
		Show: &WatchlistShow{
			ID:           row.ID,
			Name:         row.Title,
			FirstAirDate: row.FirstAirDate,
		},
		// Movie stays nil and so serializes as an explicit null.
	}
	e.Poster, e.PosterFull = common.ShowLinker(row.ID, s.ShowFiles(row.ID)).Poster(row.PosterPath)
	// The community score lives only in the snapshot. Tolerant, like every other
	// snapshot read: a corrupt body renders as a card without a score.
	var snap common.MediaSnapshot
	_ = json.Unmarshal([]byte(row.MetadataJSON), &snap)
	if snap.VoteAverage > 0 {
		v := snap.VoteAverage
		e.Rating = &v
	}
	return e, nil
}
