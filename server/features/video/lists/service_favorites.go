package lists

import (
	"context"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
)

// Favorites (D-030): the third independent per-user axis for movies and shows,
// beside the watchlist (D-011) and owned copies (D-021). Marking a favorite
// says "this one matters to me" and nothing about having watched it — a
// favorite can be unwatched, in progress, finished or dropped, and the status
// CHECKs never learn the word. Three rules hold the axis together:
//
//   - A favorite is a TRACKING ACTION and a D-008 reference: adding an
//     untracked title materialises it and syncs its full asset set, exactly as
//     watchlisting does (D-015), so a favorites-only item renders from its
//     snapshot with TMDB down and survives the sweep.
//   - A favorite row alone puts the item on the scheduled refresh's ACTIVE
//     cadence (D-025) — the same "I am still interested" signal a watchlist
//     row is.
//   - It mirrors to the user's linked TMDB account through the D-029 outbox,
//     exactly like the watchlist; the link is the opt-in, there is no
//     per-action question.
//
// The favorites page is the watchlist page's shape without the matching-users
// intersection (D-028 is watchlist-only), so the entry type, the two
// projection helpers and the parameter parser are shared rather than copied.

// FavoriteEntry is one card on the favorites page: the watchlist card's shape
// exactly, with AddedAt the instant the title was favorited.
type FavoriteEntry = WatchlistEntry

// FavoritesParams carries GET /video/favorites' query parameters as received;
// Favorites defaults and validates them so the handler stays free of domain
// branching. No match_user: intersecting lists is the watchlist's feature.
type FavoritesParams struct {
	OrderBy string // "" | added | title | release_date
	Order   string // "" | asc | desc
	Type    string // "" | all | movie | show
	Limit   string // positive integer; "" = unlimited
	Offset  string // non-negative integer; "" = 0
}

// FavoriteAdd marks a movie or show as one of this user's favorites. Adding
// an already-favorited target is a no-op reported as created=false. For a
// user with a working TMDB link the addition is also queued for their TMDB
// favorites, in the same transaction (D-029); the enqueue decides that in its
// SQL, so there is no branch here.
func (s *Service) FavoriteAdd(ctx context.Context, userID int64, p common.TrackParams) (common.TrackResult, error) {
	target, err := s.ResolveTrackedTarget(ctx, p, common.FavoriteTargets)
	if err != nil {
		return common.TrackResult{}, err
	}

	now := s.Now().UTC().Format(time.RFC3339)
	var created bool
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		var txErr error
		created, txErr = common.InsertFavorite(ctx, tx, userID, target.TargetType, target.TargetID, now)
		if txErr != nil {
			return txErr
		}
		return common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindFavorite, target.TargetType, target.TargetID, "set", nil, now)
	})
	if err != nil {
		return common.TrackResult{}, err
	}
	s.KickTMDBSync()
	s.SyncTargetAssets(ctx, target)
	return common.TrackResult{Created: created, TargetType: target.TargetType, TargetID: target.TargetID}, nil
}

// FavoriteRemove takes a target out of this user's favorites. Idempotent —
// removing what is not favorited is a success — and never a provider call on
// this request: for a linked user the removal is queued for their TMDB
// favorites (D-029). The catalog row and its assets stay: untracking never
// hard-deletes (D-008).
func (s *Service) FavoriteRemove(ctx context.Context, userID int64, targetType, targetID string) error {
	id, err := common.ParsePathTarget(targetType, targetID, common.FavoriteTargets)
	if err != nil {
		return err
	}
	now := s.Now().UTC().Format(time.RFC3339)
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if err := deleteFavorite(ctx, tx, userID, targetType, id); err != nil {
			return err
		}
		return common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindFavorite, targetType, id, "delete", nil, now)
	})
	if err != nil {
		return err
	}
	s.KickTMDBSync()
	return nil
}

// Favorites is GET /video/favorites: one ordered, paged list of this user's
// favorites, movies and shows interleaved, each projected the way the
// watchlist projects its cards. A pure local read — never a provider call.
func (s *Service) Favorites(ctx context.Context, userID int64, p FavoritesParams) ([]FavoriteEntry, error) {
	page, err := parseListPage(p.OrderBy, p.Order, p.Type, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	targets, err := listFavoriteTargets(ctx, s.DB, favoritesFilter{
		UserID:  userID,
		OrderBy: page.OrderBy,
		Type:    page.Type,
		Desc:    page.Desc,
		Limit:   page.Limit,
		Offset:  page.Offset,
	})
	if err != nil {
		return nil, err
	}
	return s.projectListEntries(ctx, userID, targets)
}
