package watches

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// The per-operation target policy, stated once each so the three call sites
// cannot drift — the duplicated `!= "movie"` string compares they replace had
// already been copied three times.
var (
	// 'show' is deliberately absent: marking a show watched is a bulk
	// operation with a count for an answer, so it has its own route.
	watchableTargets = []string{"movie", "episode"}
)

// trackShowState reads back the owning show's status and derived progress for
// the response. Best-effort: a read failure loses a UI hint, never the write.
func (s *Service) trackShowState(ctx context.Context, userID int64, show common.ShowRow, today string) *common.TrackShow {
	out := &common.TrackShow{ID: show.ID}
	if st, err := common.UserShowFlags(ctx, s.DB, userID, show.ID); err == nil {
		out.Status = st.Status
	}
	if p, err := common.ShowProgress(ctx, s.DB, userID, show.ID, today); err == nil {
		out.Progress = p
	}
	return out
}

// resolveWatchedAt turns the optional request field into the instant to store
// and says which insert mode the caller asked for. The stored string is always
// re-formatted rather than passed through: SPEC §4 requires one UTC instant
// with no offset, and a client sending a local offset or the milliseconds
// Date.toISOString() emits would otherwise break the plain string ordering the
// feed relies on.
func resolveWatchedAt(raw *string, now time.Time) (at string, logged bool, err error) {
	if raw == nil {
		return now.Format(time.RFC3339), false, nil
	}
	at, err = common.NormaliseInstant(*raw, now)
	switch {
	case errors.Is(err, common.ErrInstantFuture):
		return "", false, fmt.Errorf(
			"%w: watched_at cannot be in the future", httpx.ErrValidation)
	case err != nil:
		return "", false, fmt.Errorf(
			"%w: watched_at must be an RFC3339 timestamp", httpx.ErrValidation)
	}
	return at, true, nil
}

// WatchParams is the body of POST /video/watches. TrackParams is shared with
// the watchlist and rating writes, so the two fields only a watch can carry sit
// here instead — the BulkWatchParams arrangement. The embedded fields flatten
// in JSON, so the body stays
// {target_type, tmdb_id|target_id, season?, episode?, watched_at?, rating?}.
type WatchParams struct {
	common.TrackParams
	// The instant the watch happened, RFC3339. Omitting it means "now" and
	// keeps the idempotent first-mark behaviour an optimistic retry or a plain
	// API client needs. Supplying it makes the call an explicit watch
	// LOG: the row is always written, even when the target already has watches
	// — D-009's deliberate rewatch, which is a new dated row by design. A value
	// in the future is a 422; there is no lower bound, because a watch really
	// can predate the library.
	WatchedAt *string `json:"watched_at"`
	// Optional 1-10, stored in the same transaction as the watch row so a
	// confirmed "I watched it and thought this" cannot half-succeed. Omitting
	// it leaves any existing rating alone — this is never a clear; that is
	// DELETE /video/ratings.
	Rating *int `json:"rating"`
}

// MarkWatched records a watch of a movie or an episode for this user, in one of
// two modes the request itself chooses (see WatchParams.WatchedAt):
//
//   - no watched_at — the first-watch mark. Idempotent: a target already in the
//     user's history returns created=false and writes nothing, so a double tap
//     or a retry cannot manufacture a rewatch.
//   - with watched_at — the deliberate log of one viewing at one instant
//     (D-009). Always inserts, so created is always true, and the instant may
//     be in the past.
//
// The watch row, the optional rating and the status the watch implies are one
// transaction (D-010), and the status recompute runs exactly once.
//
// Backdating moves the row's watched_at and NOTHING else: the state row's
// timestamp and D-012's aired-episode predicate both keep taking the real
// clock, or a watch logged for last year would decide which episodes had aired
// by then. The recompute reads the log, so the backdated row still counts.
func (s *Service) MarkWatched(ctx context.Context, userID int64, p WatchParams) (common.TrackResult, error) {
	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return common.TrackResult{}, err
	}
	at := clk.At
	watchedAt, logged, err := resolveWatchedAt(p.WatchedAt, at)
	if err != nil {
		return common.TrackResult{}, err
	}
	if p.Rating != nil {
		if err := common.AssertRatingInRange(*p.Rating); err != nil {
			return common.TrackResult{}, err
		}
	}

	target, err := s.ResolveTrackedTarget(ctx, p.TrackParams, watchableTargets)
	if err != nil {
		return common.TrackResult{}, err
	}

	now, today := clk.Now, clk.Today

	var created bool
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if logged {
			if txErr := common.InsertWatch(ctx, tx, userID, target.TargetType, target.TargetID, watchedAt, "watch"); txErr != nil {
				return txErr
			}
			created = true
		} else {
			var txErr error
			created, txErr = insertWatchIfNever(ctx, tx, userID, target.TargetType, target.TargetID, watchedAt, "watch")
			if txErr != nil {
				return txErr
			}
		}
		if p.Rating != nil {
			// updated_at is the real now, not the backdated instant: a rating
			// is the current opinion, whenever the viewing was.
			if txErr := common.UpsertVideoRating(ctx, tx, userID, target.TargetType, target.TargetID, *p.Rating, now); txErr != nil {
				return txErr
			}
			if txErr := common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindRating, target.TargetType, target.TargetID, "set", p.Rating, now); txErr != nil {
				return txErr
			}
		}
		if target.TargetType == "episode" {
			return common.RecomputeShowStatus(ctx, tx, userID, target.Show.ID, today, now)
		}
		return common.RecomputeMovieStatus(ctx, tx, userID, target.TargetID, now)
	})
	if err != nil {
		return common.TrackResult{}, err
	}
	if p.Rating != nil {
		s.KickTMDBSync()
	}
	s.SyncTargetAssets(ctx, target)

	res := common.TrackResult{Created: created, TargetType: target.TargetType, TargetID: target.TargetID}
	if target.TargetType == "episode" {
		res.Show = s.trackShowState(ctx, userID, *target.Show, today)
	}
	return res, nil
}

// DeleteWatch removes one row from this user's watch log — the correction path
// for a mis-marked or duplicated watch.
//
// NOT idempotent, unlike WatchlistRemove and RatingRemove: those mean "ensure X
// is not in this set", while this addresses one specific row by its own
// identity, so a second call is a 404 rather than a quiet success. A row that
// belongs to another user is the same 404 — the store scopes on user_id, and
// ErrNotFound covers both cases by design.
//
// The delete and the status it implies are one transaction (D-010), and the
// recompute runs exactly once. Removing the last watch is what makes the
// recompute's collapse branch reachable: a movie's state row disappears (it
// becomes unwatched again), a show's falls back to 'watching' or disappears
// too, and a user-set 'dropped' survives because it is not derived.
//
// The catalog row and its stored artwork stay even when this was the last
// reference: untracking never hard-deletes (D-008); the deferred sweep collects
// unreferenced rows once it lands. Nothing is queued here, matching the two
// existing removal paths.
func (s *Service) DeleteWatch(ctx context.Context, userID int64, watchID string) error {
	// No target_type to check against an allow-list, so this parses the id
	// directly rather than bending ParsePathTarget, which exists for the
	// polymorphic pair.
	id, err := strconv.ParseInt(watchID, 10, 64)
	if err != nil || id < 1 {
		return fmt.Errorf("%w: id must be a positive integer", httpx.ErrValidation)
	}

	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return err
	}
	now, today := clk.Now, clk.Today

	return s.DB.InTx(ctx, func(tx db.Tx) error {
		targetType, targetID, found, txErr := deleteWatchByID(ctx, tx, userID, id)
		if txErr != nil {
			return txErr
		}
		if !found {
			return fmt.Errorf("%w: no such watch", httpx.ErrNotFound)
		}
		if targetType == "episode" {
			showID, showErr := common.ShowIDForEpisode(ctx, tx, targetID)
			if showErr != nil {
				return showErr
			}
			return common.RecomputeShowStatus(ctx, tx, userID, showID, today, now)
		}
		return common.RecomputeMovieStatus(ctx, tx, userID, targetID, now)
	})
}

// BulkWatchParams is the body of POST /video/watches/bulk. Scope is what let
// "mark whole season watched" land as an additive widening rather than a new
// route. The scope names the target's type: scope show takes a show target,
// scope season a season target.
//
// Rewatch and WatchedAtMode are two independent axes, which is why they are two
// fields rather than one enum: a remaining-only mark can be backdated, and a
// full rewatch can happen "now". The zero values reproduce the original
// behaviour exactly, so a body that predates them still means what it did.
type BulkWatchParams struct {
	common.TrackParams
	Scope string `json:"scope"` // show
	// True drops the not-yet-watched guard: every eligible episode gets a NEW
	// watch row, watched ones included — D-009's deliberate rewatch, set-wise.
	// Like the single dated log it is NOT idempotent; the client's pending
	// state is the double-tap guard.
	Rewatch bool `json:"rewatch"`
	// Where each row's watched_at comes from: "now" (default) and "fixed" share
	// one instant; "episode_air" takes each episode's own air date and
	// "season_air" its season's premiere, both as midnight in the user's zone
	// (D-038), stored in UTC. Eligibility
	// guarantees an episode air date; a season premiere can be missing or
	// unaired, which is what WatchedAt covers there.
	WatchedAtMode string `json:"watched_at_mode"`
	// The instant for "fixed" (required), or the fallback for "season_air"
	// episodes whose season has no usable premiere date. Same rules as the
	// single log's field: RFC3339, future is a 422, no lower bound. Rejected
	// with the other two modes, where a value means the client misunderstood.
	WatchedAt *string `json:"watched_at"`
}

// BulkWatchResult is deliberately its own shape rather than a TrackResult:
// a bulk mark's honest answer to "what did you do" is a COUNT, and TrackResult
// reports a boolean. Overloading it would have meant either lying or breaking a
// published response shape.
//
// Status and Progress are always the OWNING SHOW's, whatever the scope: a
// season has no status of its own (D-010's one label is per show), and marking
// one season can complete the show.
type BulkWatchResult struct {
	TargetType string                    `json:"target_type"`
	TargetID   int64                     `json:"target_id"`
	Created    int64                     `json:"created"` // watch rows actually written
	Status     *string                   `json:"status"`
	Progress   common.ShowProgressCounts `json:"progress"`
}

// BulkMarkWatched marks every aired, non-special episode of a show — or, with
// scope season, of one season — watched: SPEC §6's "mark whole show watched"
// and "mark whole season watched", each specified as a single API call rather
// than a loop of per-episode requests.
//
// Idempotent in the default mode: running it twice writes nothing the second
// time, because the insert carries insertWatchIfNever's rule set-wise. That
// matters more here than anywhere else in the app — a double tap would
// otherwise be two hundred duplicate rows indistinguishable from deliberate
// rewatches. Rewatch=true is the one deliberate exception (see BulkWatchParams).
//
// Backdating — fixed or air-date-derived — moves the rows' watched_at and
// NOTHING else, exactly as on the single log: today and now below keep the
// real clock, so D-012's aired predicate and the state row's timestamp never
// see the past.
//
// The status is recomputed ONCE for the whole batch, not once per episode.
func (s *Service) BulkMarkWatched(ctx context.Context, userID int64, p BulkWatchParams) (BulkWatchResult, error) {
	if p.Scope != "show" && p.Scope != "season" {
		return BulkWatchResult{}, fmt.Errorf("%w: scope must be show or season", httpx.ErrValidation)
	}
	mode := p.WatchedAtMode
	if mode == "" {
		mode = "now"
	}
	switch mode {
	case "now", "episode_air":
		// A value here means the client misunderstood the contract; ignoring
		// it would hide that bug behind a plausible-looking result.
		if p.WatchedAt != nil {
			return BulkWatchResult{}, fmt.Errorf(
				"%w: watched_at is only accepted with watched_at_mode fixed or season_air", httpx.ErrValidation)
		}
	case "fixed":
		if p.WatchedAt == nil {
			return BulkWatchResult{}, fmt.Errorf(
				"%w: watched_at is required when watched_at_mode is fixed", httpx.ErrValidation)
		}
	case "season_air":
		// watched_at is the optional fallback; checked against need in the tx.
	default:
		return BulkWatchResult{}, fmt.Errorf(
			"%w: watched_at_mode must be now, fixed, episode_air or season_air", httpx.ErrValidation)
	}

	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return BulkWatchResult{}, err
	}
	at := clk.At
	// Shared instant for now/fixed, fallback for season_air; parse + future
	// check + UTC re-format are the single log's, reused. With no watched_at
	// this is just now, and the air-date modes never select it.
	watchedAt, _, err := resolveWatchedAt(p.WatchedAt, at)
	if err != nil {
		return BulkWatchResult{}, err
	}

	// The scope IS the target's type: scope show marks a show target, scope
	// season a season target, so a mismatched pair 422s in the resolver.
	target, err := s.ResolveTrackedTarget(ctx, p.TrackParams, []string{p.Scope})
	if err != nil {
		return BulkWatchResult{}, err
	}
	show := *target.Show
	// nil marks the whole show; set, it narrows every statement to one season.
	var seasonNumber *int
	if target.Season != nil {
		// Specials never gate completion (D-012), so EligibleEpisode would
		// select nothing — an honest "0 created" that reads like success.
		// Refuse instead, with the reason.
		if target.Season.SeasonNumber == 0 {
			return BulkWatchResult{}, fmt.Errorf(
				"%w: specials cannot be bulk-marked; they do not count toward completion", httpx.ErrValidation)
		}
		seasonNumber = &target.Season.SeasonNumber
	}

	now, today := clk.Now, clk.Today

	var created int64
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if mode == "season_air" && p.WatchedAt == nil {
			// Eligibility guarantees each EPISODE an air date; its season's
			// premiere can still be missing, or itself unaired (TMDB dirt).
			// Either would need the fallback, so its absence is the client's
			// error — and the tx aborts before a single row lands. Scoped to
			// one season this inspects only that season's premiere.
			n, txErr := common.CountEligibleWithoutSeasonDate(ctx, tx, show.TMDBID, show.ID, seasonNumber, today)
			if txErr != nil {
				return txErr
			}
			if n > 0 {
				return fmt.Errorf(
					"%w: one or more seasons have no premiere date; supply watched_at as a fallback", httpx.ErrValidation)
			}
		}
		var txErr error
		created, txErr = common.BulkMarkShowWatched(ctx, tx, userID, show, seasonNumber, mode, watchedAt, clk, p.Rewatch)
		if txErr != nil {
			return txErr
		}
		// Whole-show even for a season scope: status is the show's one label,
		// and marking a season can complete the show.
		return common.RecomputeShowStatus(ctx, tx, userID, show.ID, today, now)
	})
	if err != nil {
		return BulkWatchResult{}, err
	}
	s.SyncShowAssets(ctx, show)

	state := s.trackShowState(ctx, userID, show, today)
	return BulkWatchResult{
		TargetType: target.TargetType,
		TargetID:   target.TargetID,
		Created:    created,
		Status:     state.Status,
		Progress:   state.Progress,
	}, nil
}
