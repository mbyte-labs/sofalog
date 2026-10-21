package ratings

import (
	"context"
	"fmt"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// RateParams is the body of PUT /video/ratings. It embeds TrackParams so the
// target-resolution rules — the allowed target types, exactly one of tmdb_id
// and target_id — are stated once and cannot drift from the other tracking
// calls.
// The embedded fields flatten in JSON, so the body stays
// {target_type, tmdb_id|target_id, rating}.
type RateParams struct {
	common.TrackParams
	// A pointer so an omitted rating is a validation error rather than a
	// silent 0, which the column's CHECK would reject anyway.
	Rating *int `json:"rating"`
}

// RatingResult reports the rating now stored. There is no created flag: a
// re-rating overwrites (D-009), so "already existed" is not a distinction the
// caller can act on. TargetID is the local catalog id — for a movie rated from
// a live details view, the id its freshly materialised row received.
type RatingResult struct {
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	Rating     int    `json:"rating"`
}

// Rate stores this user's rating of a movie, a show or an episode, replacing
// any previous one. A SEASON cannot be rated (D-020) — the refusal is one entry
// in RateableTargets, not a branch here and not a narrowed CHECK.
//
// A rating is a reference (D-008), so rating tracks: a tmdb_id with no catalog
// row materialises one from TMDB and its media set is enqueued, exactly as
// WatchlistAdd does. Rating one episode of an untracked show therefore pulls
// the whole show local. With TMDB down that first rating is a 503 and nothing
// is written.
//
// Rating deliberately does NOT touch either status table: watch status is
// derived from the watch log alone (D-010), and an opinion about something is
// not a claim to have watched it.
func (s *Service) Rate(ctx context.Context, userID int64, p RateParams) (RatingResult, error) {
	if p.Rating == nil {
		return RatingResult{}, fmt.Errorf("%w: rating is required", httpx.ErrValidation)
	}
	if err := common.AssertRatingInRange(*p.Rating); err != nil {
		return RatingResult{}, err
	}

	target, err := s.ResolveTrackedTarget(ctx, p.TrackParams, common.RateableTargets)
	if err != nil {
		return RatingResult{}, err
	}

	now := s.Now().UTC().Format(time.RFC3339)
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if err := common.UpsertVideoRating(ctx, tx, userID, target.TargetType, target.TargetID, *p.Rating, now); err != nil {
			return err
		}
		// The one-way push to the user's TMDB account (D-029): queued in the
		// same transaction, sent by the worker; a no-op for an unlinked user.
		return common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindRating, target.TargetType, target.TargetID, "set", p.Rating, now)
	})
	if err != nil {
		return RatingResult{}, err
	}
	s.KickTMDBSync()
	s.SyncTargetAssets(ctx, target)
	return RatingResult{TargetType: target.TargetType, TargetID: target.TargetID, Rating: *p.Rating}, nil
}

// RatingRemove clears this user's rating of a target. Idempotent — clearing
// what was never rated is a success — and never a provider call on this path:
// a linked user's removal is queued for the background push (D-029).
// The catalog row and its stored artwork stay even when this was the last
// reference: untracking never hard-deletes (D-008); the deferred sweep
// collects unreferenced rows once it lands.
func (s *Service) RatingRemove(ctx context.Context, userID int64, targetType, targetID string) error {
	id, err := common.ParsePathTarget(targetType, targetID, common.RateableTargets)
	if err != nil {
		return err
	}
	now := s.Now().UTC().Format(time.RFC3339)
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if err := deleteVideoRating(ctx, tx, userID, targetType, id); err != nil {
			return err
		}
		// Removing here removes there (D-029) — for a linked user.
		return common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindRating, targetType, id, "delete", nil, now)
	})
	if err != nil {
		return err
	}
	s.KickTMDBSync()
	return nil
}
