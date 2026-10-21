package copies

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// The copies target policy, beside its siblings in service_tracking.go for the
// same reason: stated once so the add and remove paths cannot drift.
//
// 'show' is a bulk fan-out SCOPE, never a stored target: a show is not itself
// "owned on DVD" — its seasons are, so a show-scoped call writes or removes one
// row per non-special season (D-021). 'episode' is absent on purpose — a copy
// is a disc or a library entry, and nobody shelves one episode.
var copyTargets = []string{"movie", "show", "season"}

// The copy kinds the UI offers, and the enforceable subset of the kind CHECK in
// user_video_copies. The 422 message is generated from this list, like
// assertAllowedTarget's.
var copyKinds = []string{"jellyfin", "dvd", "bluray", "uhd", "physical", "digital"}

// assertAllowedKind refuses an unknown kind — and, while a Jellyfin server is
// configured, the `jellyfin` kind itself: the scan owns it then (D-033), and
// a hand-set row would be overwritten by the next scan anyway, so accepting it
// would only lie for a few hours.
func (s *Service) assertAllowedKind(kind string) error {
	if !slices.Contains(copyKinds, kind) {
		return fmt.Errorf("%w: kind must be one of %s", httpx.ErrValidation,
			strings.Join(copyKinds, ", "))
	}
	if kind == "jellyfin" && s.JellyfinSynced() {
		return fmt.Errorf("%w: the Jellyfin copy is managed by the Jellyfin library scan", httpx.ErrValidation)
	}
	return nil
}

// CopyParams is a TrackParams plus which kind of copy — embedding flattens the
// JSON, so the body is the tracking envelope with one extra field.
type CopyParams struct {
	common.TrackParams
	Kind string `json:"kind"`
}

// CopyResult reports what a copies write did. Created is a COUNT, not
// TrackResult's bool, because the show scope fans out: 0 or 1 for a movie or
// season target, 0..N seasons for a show — the BulkWatchResult precedent.
// TargetID is the local catalog id, which a page navigated by tmdb_id adopts.
type CopyResult struct {
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
	Kind       string `json:"kind"`
	Created    int64  `json:"created"`
}

// CopyAdd records that the user holds a copy of the target — an independent
// axis like the watchlist (D-011), never a status. A copy is a tracking action:
// an untracked target addressed by tmdb_id is materialised (D-015), and the
// row is a D-008 reference that keeps the item and its artwork alive on its
// own. Adding a kind the target already has is a no-op reported as created=0.
func (s *Service) CopyAdd(ctx context.Context, userID int64, p CopyParams) (CopyResult, error) {
	if err := s.assertAllowedKind(p.Kind); err != nil {
		return CopyResult{}, err
	}
	target, err := s.ResolveTrackedTarget(ctx, p.TrackParams, copyTargets)
	if err != nil {
		return CopyResult{}, err
	}

	now := s.Now().UTC().Format(time.RFC3339)
	var created int64
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if target.TargetType == "show" {
			var txErr error
			created, txErr = insertSeasonCopiesForShow(ctx, tx, userID, target.TargetID, p.Kind, now)
			return txErr
		}
		one, txErr := insertCopy(ctx, tx, userID, target.TargetType, target.TargetID, p.Kind, now)
		if one {
			created = 1
		}
		return txErr
	})
	if err != nil {
		return CopyResult{}, err
	}
	s.SyncTargetAssets(ctx, target)
	return CopyResult{
		TargetType: target.TargetType,
		TargetID:   target.TargetID,
		Kind:       p.Kind,
		Created:    created,
	}, nil
}

// CopyRemove takes one kind off the user's copies of the target — for the show
// scope, off every non-special season, mirroring the fan-out add. Idempotent:
// removing what is not held is a success, and it never touches TMDB. The
// catalog row and its assets stay even when this was the last reference:
// untracking never hard-deletes (D-008); the deferred sweep collects
// unreferenced rows once it lands. Nothing is queued here, matching the
// watchlist and rating removals.
func (s *Service) CopyRemove(ctx context.Context, userID int64, targetType, targetID, kind string) error {
	id, err := common.ParsePathTarget(targetType, targetID, copyTargets)
	if err != nil {
		return err
	}
	if err := s.assertAllowedKind(kind); err != nil {
		return err
	}
	return s.DB.InTx(ctx, func(tx db.Tx) error {
		if targetType == "show" {
			return deleteSeasonCopiesForShow(ctx, tx, userID, id, kind)
		}
		return deleteCopy(ctx, tx, userID, targetType, id, kind)
	})
}
