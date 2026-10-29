package watches

import (
	"context"
	"fmt"
	"strconv"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// Dropping a show — the one status the user sets by hand (D-010), and the only
// one that is intent rather than a derivation. Shows only: a movie's CHECK
// permits 'watched' and nothing else, so there is no dropped movie in any
// milestone (SPEC §6).
//
// Neither operation is a tracking action, which is what makes this file the
// odd one out among the writes: WatchlistAdd and CopyAdd materialise an
// untracked item from TMDB (D-015), while a drop only ever touches a show the
// user has already STARTED. A state row exists if and only if the log holds an
// episode watch or the user already dropped the show, so the row is always
// there by the time either of these runs — which is why both address the show
// by its local id, never a tmdb_id, and why no path here can 503.

// DropResult is the answer to a drop or an un-drop. Changed says whether the
// stored status actually moved, so a repeat of either operation is honest about
// having done nothing — the WatchlistAdd created=false shape.
//
// Show carries the state AFTER the write, and it is the reason the un-drop
// answers with a body at all: releasing a drop recomputes from the log, and
// whether that lands on 'watched', 'watching' or no row at all is not something
// the client can derive (D-010 — the label is the server's to state).
type DropResult struct {
	Changed bool             `json:"changed"`
	Show    common.TrackShow `json:"show"`
}

// parseShowPathID validates the {id} both drop routes carry. Not
// ParsePathTarget: that one exists for the polymorphic (target_type, target_id)
// pair, and these routes name their media type in the path instead — dropping
// is show-only work, so there is no target type to check against an allow-list.
func parseShowPathID(showID string) (int64, error) {
	id, err := strconv.ParseInt(showID, 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("%w: id must be a positive integer", httpx.ErrValidation)
	}
	return id, nil
}

// DropShow marks a show dropped: "I gave up on this one". Sticky by design — no
// recompute, no newly aired episode and no later watch clears it, only
// UndropShow does (D-010). A dropped show is excluded from Up Next, and the
// exclusion is already in that query rather than here.
//
// Only a STARTED show can be dropped. Without at least one episode watch there
// is no state row, and a drop that created one would be inventing a watch status
// for a show the user never began — the same reason watchlisting and rating
// deliberately leave the status absent. That case is a 422, not a silent write.
//
// Idempotent: dropping an already-dropped show reports changed=false, writes
// nothing, and leaves updated_at where it was.
//
// Nothing is materialised and no artwork is synced, because nothing was
// downloaded: this never leaves the local database.
func (s *Service) DropShow(ctx context.Context, userID int64, showID string) (DropResult, error) {
	id, err := parseShowPathID(showID)
	if err != nil {
		return DropResult{}, err
	}
	show, err := common.GetShowByID(ctx, s.DB, id)
	if err != nil {
		return DropResult{}, err
	}

	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return DropResult{}, err
	}
	now, today := clk.Now, clk.Today

	state, err := common.UserShowFlags(ctx, s.DB, userID, show.ID)
	if err != nil {
		return DropResult{}, err
	}
	switch {
	case state.Status == nil:
		return DropResult{}, fmt.Errorf(
			"%w: only a show you have started watching can be dropped", httpx.ErrValidation)
	case *state.Status == "dropped":
		// Already there. The UPDATE would report this too, but reading it first
		// is what separates it from the no-row case above.
		return DropResult{Changed: false, Show: *s.trackShowState(ctx, userID, show, today)}, nil
	}

	var changed bool
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		var txErr error
		changed, txErr = common.SetShowDropped(ctx, tx, userID, show.ID, now)
		return txErr
	})
	if err != nil {
		return DropResult{}, err
	}
	return DropResult{Changed: changed, Show: *s.trackShowState(ctx, userID, show, today)}, nil
}

// UndropShow releases a drop and recomputes what the watch log implies, in one
// transaction: a show dropped after finishing comes back as 'watched' rather
// than 'watching', and one whose watches have since been deleted loses its row
// entirely (SPEC §6).
//
// Idempotent, like WatchlistRemove: un-dropping a show that is not dropped is a
// success that changed nothing, and it deliberately skips the recompute — this
// operation may only ever release the drop, never rewrite a status the user did
// not set.
func (s *Service) UndropShow(ctx context.Context, userID int64, showID string) (DropResult, error) {
	id, err := parseShowPathID(showID)
	if err != nil {
		return DropResult{}, err
	}
	show, err := common.GetShowByID(ctx, s.DB, id)
	if err != nil {
		return DropResult{}, err
	}

	// One clock reading for both, exactly as the watch writes do it, so the
	// recompute's idea of "aired" and the row's timestamp cannot disagree.
	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return DropResult{}, err
	}
	now, today := clk.Now, clk.Today

	var changed bool
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		var txErr error
		changed, txErr = common.ClearShowDropped(ctx, tx, userID, show.ID, now)
		if txErr != nil || !changed {
			return txErr
		}
		return common.RecomputeShowStatus(ctx, tx, userID, show.ID, today, now)
	})
	if err != nil {
		return DropResult{}, err
	}
	return DropResult{Changed: changed, Show: *s.trackShowState(ctx, userID, show, today)}, nil
}
