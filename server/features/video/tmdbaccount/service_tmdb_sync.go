package tmdbaccount

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// The one-way push to the user's own TMDB account (D-029) — SPEC §5's sixth
// provider call site and the only WRITE to a provider.
//
// Three parts. The LINK: a user sends their browser to TMDB, approves a request
// token, and comes back; the exchange yields a long-lived session that is
// stored per user. The link is the consent; it queues nothing by itself. The
// SETUP (D-036, SetupTMDBSync): once per link the user says, per kind —
// ratings, watchlist, favorites — whether to pull TMDB's list into the library
// once (service_tmdb_pull.go), push what sofalog already holds once (the
// backfills), or skip, and sets the kind's ongoing switch. The OUTBOX: every
// local rating write (Rate, RatingRemove, a watch logged with a rating, a
// restored rating), every watchlist write (WatchlistAdd, WatchlistRemove, a
// restored listing) and every favorite write (FavoriteAdd, FavoriteRemove —
// D-030) enqueues a row in the SAME transaction, guarded in SQL by the link's
// existence AND that kind's switch, and a worker owned by the Service drains
// it in the background. TMDB being down never slows or fails a local action;
// the rows are durable, so a restart resumes where it stopped.
//
// Local is authoritative. After the setup nothing is read back from the
// account — Account is called once at link time for the username — and
// unlinking drops the pending rows and revokes the session but leaves TMDB's
// ratings as they are.

const (
	// tmdbLinkFlowTTL bounds the round trip through TMDB's approval page. TMDB
	// gives the request token an hour; ours expires first, so a stale cookie
	// is always our "expired", never TMDB's.
	tmdbLinkFlowTTL = 10 * time.Minute

	// tmdbSyncBootDelay is how long after start the worker first looks: long
	// enough for the process to settle, short enough that rows a restart left
	// behind go out promptly.
	tmdbSyncBootDelay = 5 * time.Second
	// tmdbSyncIdlePoll is the fallback wake-up that fires backoff retries when
	// no write has kicked the worker.
	tmdbSyncIdlePoll = time.Minute
	// tmdbSyncRetryBase doubles per failed attempt up to tmdbSyncRetryMax.
	// There is no terminal state for an outage: an outage is not the row's
	// fault, and one call per hour per stuck row is the cost of never losing
	// a rating.
	tmdbSyncRetryBase = 30 * time.Second
	tmdbSyncRetryMax  = time.Hour
	// tmdbSyncBatch is how many rows one pass sends before re-reading the
	// queue; a full batch loops immediately.
	tmdbSyncBatch = 50
)

// TMDBLinkFlow is the in-progress link, carried in a browser cookie between the
// start and the callback. UserID binds the flow to the session that started
// it, RequestToken to the token TMDB will hand back.
type TMDBLinkFlow struct {
	UserID       int64     `json:"u"`
	RequestToken string    `json:"t"`
	Expires      time.Time `json:"e"`
}

// TMDBLinkStatus is GET /video/tmdb/link: one shape whether linked or not. The
// session id is deliberately absent — it never leaves the server.
type TMDBLinkStatus struct {
	// Configured is whether the server holds a TMDB credential at all; false
	// means Link cannot work and the SPA hides the button.
	Configured bool `json:"configured"`
	Linked     bool `json:"linked"`
	// Broken is set when TMDB refused the stored session: the row is kept so
	// the page can name the account, but no new ratings are queued until the
	// user links again.
	Broken   bool    `json:"broken"`
	Username *string `json:"username"`
	LinkedAt *string `json:"linked_at"`
	// Pending is how many changes (ratings, watchlist and favorites) are
	// still waiting to be sent.
	Pending int `json:"pending"`
	// SetupDone is whether the one-time setup after linking has finished
	// (D-036); false means the SPA shows the setup step. Sync holds the three
	// ongoing switches, all false when unlinked. Pull is the setup's pull
	// job for THIS link — running, or the last one's report — and null when
	// none has run since boot.
	SetupDone bool          `json:"setup_done"`
	Sync      TMDBSyncKinds `json:"sync"`
	Pull      *TMDBPullJob  `json:"pull"`
}

// TMDBSyncKinds is the three ongoing-sync switches (D-036): whether a local
// write of that kind queues a push. They govern FUTURE writes only.
type TMDBSyncKinds struct {
	Ratings   bool `json:"ratings"`
	Watchlist bool `json:"watchlist"`
	Favorites bool `json:"favorites"`
}

// The three things the setup can do with one kind (D-036).
const (
	tmdbSetupPull = "pull"
	TMDBSetupPush = "push"
	tmdbSetupSkip = "skip"
)

// TMDBSetupSection is one kind's setup choice: the one-time action and the
// ongoing switch.
type TMDBSetupSection struct {
	Action string `json:"action"` // pull | push | skip
	Sync   bool   `json:"sync"`
}

// TMDBSetupParams is POST /video/tmdb/link/setup.
type TMDBSetupParams struct {
	Ratings   TMDBSetupSection `json:"ratings"`
	Watchlist TMDBSetupSection `json:"watchlist"`
	Favorites TMDBSetupSection `json:"favorites"`
}

// errTMDBLinkDenied is the user refusing on TMDB's approval page. Unexported:
// the handler maps it to its result code, nothing else sees it.
var errTMDBLinkDenied = errors.New("tmdb link denied by the user")

// StartTMDBLink asks TMDB for a request token and returns the approval URL the
// browser must be sent to, plus the flow the handler stores in a cookie.
func (s *Service) StartTMDBLink(ctx context.Context, userID int64) (string, TMDBLinkFlow, error) {
	if s.TMDB == nil || !s.TMDB.Configured() {
		return "", TMDBLinkFlow{}, fmt.Errorf("%w: no TMDB credential configured", httpx.ErrProviderUnavailable)
	}
	token, err := s.TMDB.NewRequestToken(ctx)
	if err != nil {
		return "", TMDBLinkFlow{}, err
	}
	flow := TMDBLinkFlow{UserID: userID, RequestToken: token, Expires: s.Now().UTC().Add(tmdbLinkFlowTTL)}
	return s.TMDB.AuthenticateURL(token, s.TMDBLinkRedirectURI), flow, nil
}

// CompleteTMDBLink finishes the flow after TMDB redirected the browser back.
// The flow must belong to this user, be unexpired, and name the token TMDB
// returned — anything else is ErrValidation ("expired" to the user). A denial
// is errTMDBLinkDenied. On success the session is exchanged, the account read
// and the link written. Nothing is queued: the setup (SetupTMDBSync) decides
// what happens to what the user already holds. Re-linking the same account
// keeps the setup and the switches; a DIFFERENT account resets them and drops
// the old account's pending rows, which must not be sent to the new one. A
// session this link replaces is revoked best-effort afterwards.
func (s *Service) CompleteTMDBLink(ctx context.Context, userID int64, flow TMDBLinkFlow, requestToken string, approved bool) error {
	now := s.Now().UTC()
	switch {
	case flow.UserID != userID:
		return fmt.Errorf("%w: the link was started by another session", httpx.ErrValidation)
	case !now.Before(flow.Expires):
		return fmt.Errorf("%w: the link request expired", httpx.ErrValidation)
	case requestToken == "" || requestToken != flow.RequestToken:
		return fmt.Errorf("%w: the returned token does not match the link request", httpx.ErrValidation)
	}
	if !approved {
		return errTMDBLinkDenied
	}
	if s.TMDB == nil || !s.TMDB.Configured() {
		return fmt.Errorf("%w: no TMDB credential configured", httpx.ErrProviderUnavailable)
	}

	sessionID, err := s.TMDB.CreateSession(ctx, requestToken)
	if err != nil {
		if errors.Is(err, provider.ErrSessionInvalid) {
			// TMDB refused the token: the approval did not happen, whatever
			// the query said.
			return errTMDBLinkDenied
		}
		return err
	}
	account, err := s.TMDB.Account(ctx, sessionID)
	if err != nil {
		return err
	}

	previous, err := getTMDBAccount(ctx, s.DB, userID)
	if err != nil && !errors.Is(err, httpx.ErrNotFound) {
		return err
	}

	at := now.Format(time.RFC3339)
	differentAccount := previous.SessionID != "" && previous.AccountID != account.ID
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if err := upsertTMDBAccount(ctx, tx, userID, sessionID, account.ID, account.Username, at); err != nil {
			return err
		}
		if !differentAccount {
			return nil
		}
		if err := resetTMDBSyncSetup(ctx, tx, userID); err != nil {
			return err
		}
		return deleteTMDBSyncForUser(ctx, tx, userID)
	})
	if err != nil {
		return err
	}
	slog.Info("tmdb link: account linked", "user_id", userID, "reset_setup", differentAccount)

	if previous.SessionID != "" && previous.SessionID != sessionID && previous.BrokenAt == nil {
		if err := s.TMDB.DeleteSession(ctx, previous.SessionID); err != nil {
			slog.Warn("tmdb link: revoking the replaced session failed", "user_id", userID, "err", err)
		}
	}
	return nil
}

// UnlinkTMDB removes the link and every pending row, then revokes the session
// at TMDB (best effort; skipped when TMDB already refused it). Idempotent —
// unlinking what was never linked is a success. TMDB's ratings are left as
// they are: the account is the user's, not sofalog's.
func (s *Service) UnlinkTMDB(ctx context.Context, userID int64) error {
	row, err := getTMDBAccount(ctx, s.DB, userID)
	if errors.Is(err, httpx.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	// A setup pull still running belongs to this link: stop it so it cannot
	// finish a setup the user just walked away from.
	s.tmdbPull.cancel(userID)
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if err := deleteTMDBSyncForUser(ctx, tx, userID); err != nil {
			return err
		}
		return deleteTMDBAccount(ctx, tx, userID)
	})
	if err != nil {
		return err
	}
	if row.BrokenAt == nil && s.TMDB != nil && s.TMDB.Configured() {
		if err := s.TMDB.DeleteSession(ctx, row.SessionID); err != nil {
			slog.Warn("tmdb link: revoking the session failed", "user_id", userID, "err", err)
		}
	}
	return nil
}

// TMDBLinkStatus is a pure local read: the username was stored at link time,
// so this never reaches TMDB.
func (s *Service) TMDBLinkStatus(ctx context.Context, userID int64) (TMDBLinkStatus, error) {
	out := TMDBLinkStatus{Configured: s.TMDB != nil && s.TMDB.Configured()}
	row, err := getTMDBAccount(ctx, s.DB, userID)
	if errors.Is(err, httpx.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return TMDBLinkStatus{}, err
	}
	out.Linked = true
	out.Broken = row.BrokenAt != nil
	out.Username = &row.Username
	out.LinkedAt = &row.LinkedAt
	out.SetupDone = row.SetupDone
	out.Sync = row.Sync
	out.Pending, err = countTMDBSyncPending(ctx, s.DB, userID)
	if err != nil {
		return TMDBLinkStatus{}, err
	}
	// A job started before this link belongs to a previous one (unlinked,
	// then linked again): its report would only confuse the fresh setup.
	if job, ok := s.tmdbPull.get(userID); ok {
		snap := job.snapshot()
		if snap.StartedAt >= row.LinkedAt {
			out.Pull = &snap
		}
	}
	return out, nil
}

// SetupTMDBSync is the one-time step after linking (D-036). Per kind the user
// chose pull (read TMDB's list once, as a background job), push (queue what
// sofalog holds — the backfills), or skip, and set the kind's ongoing switch.
// The switches and the push rows land in ONE transaction; a pull runs as a job
// the SPA polls through TMDBLinkStatus. setup_done is written here only when
// nothing pulls — otherwise at the end of the pull job, so an interrupted
// pull leaves the setup to be re-run. Refused when not linked (404), broken
// (409 — link again first), already set up (409) or while a pull runs (409).
func (s *Service) SetupTMDBSync(ctx context.Context, userID int64, p TMDBSetupParams) (TMDBLinkStatus, error) {
	for _, sec := range []TMDBSetupSection{p.Ratings, p.Watchlist, p.Favorites} {
		switch sec.Action {
		case tmdbSetupPull, TMDBSetupPush, tmdbSetupSkip:
		default:
			return TMDBLinkStatus{}, fmt.Errorf("%w: action must be pull, push or skip", httpx.ErrValidation)
		}
	}
	row, err := getTMDBAccount(ctx, s.DB, userID)
	if err != nil {
		return TMDBLinkStatus{}, err
	}
	if row.BrokenAt != nil {
		return TMDBLinkStatus{}, fmt.Errorf("%w: TMDB no longer accepts this link; link again first", httpx.ErrConflict)
	}
	if row.SetupDone {
		return TMDBLinkStatus{}, fmt.Errorf("%w: the setup has already been done for this link", httpx.ErrConflict)
	}
	// A pull still running IS the setup in progress, whatever this request
	// asks for; start re-checks under the lock for the pulling case.
	if job, ok := s.tmdbPull.get(userID); ok && job.running() {
		return TMDBLinkStatus{}, fmt.Errorf("%w: a TMDB import is already running for this account", httpx.ErrConflict)
	}

	now := s.Now().UTC()
	at := now.Format(time.RFC3339)
	sections := tmdbPullSections{
		Ratings:   p.Ratings.Action == tmdbSetupPull,
		Watchlist: p.Watchlist.Action == tmdbSetupPull,
		Favorites: p.Favorites.Action == tmdbSetupPull,
	}
	pulls := sections.Ratings || sections.Watchlist || sections.Favorites

	// Reserve the job slot BEFORE the transaction: a second setup while a
	// pull runs is a 409 with nothing written.
	var job *tmdbPullJob
	var cancel context.CancelFunc
	var jobCtx context.Context
	if pulls {
		jobCtx, cancel = context.WithCancel(context.WithoutCancel(ctx))
		job = newTMDBPullJob(now)
		job.cancel = cancel
		if err := s.tmdbPull.start(userID, job); err != nil {
			cancel()
			return TMDBLinkStatus{}, err
		}
	}

	var pushed int64
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		kinds := TMDBSyncKinds{Ratings: p.Ratings.Sync, Watchlist: p.Watchlist.Sync, Favorites: p.Favorites.Sync}
		if err := updateTMDBSyncKinds(ctx, tx, userID, kinds); err != nil {
			return err
		}
		if p.Ratings.Action == TMDBSetupPush {
			n, err := enqueueRatingBackfill(ctx, tx, userID, at)
			if err != nil {
				return err
			}
			pushed += n
		}
		if p.Watchlist.Action == TMDBSetupPush {
			n, err := enqueueWatchlistBackfill(ctx, tx, userID, at)
			if err != nil {
				return err
			}
			pushed += n
		}
		if p.Favorites.Action == TMDBSetupPush {
			n, err := enqueueFavoriteBackfill(ctx, tx, userID, at)
			if err != nil {
				return err
			}
			pushed += n
		}
		if pulls {
			return nil
		}
		return setTMDBSetupDone(ctx, tx, userID)
	})
	if err != nil {
		if pulls {
			job.finish(tmdbPullStateFailed, s.Now().UTC(), &TMDBPullError{Code: tmdbPullErrInternal, Message: tmdbPullInternalMessage})
			cancel()
		}
		return TMDBLinkStatus{}, err
	}
	slog.Info("tmdb sync: setup", "user_id", userID, "pushed", pushed, "pull_ratings", sections.Ratings, "pull_watchlist", sections.Watchlist, "pull_favorites", sections.Favorites)
	if pushed > 0 {
		s.KickTMDBSync()
	}

	if pulls {
		s.tmdbPull.wg.Add(1)
		go func() {
			defer s.tmdbPull.wg.Done()
			defer cancel()
			defer func() {
				// httpx.Recover guards handlers, not goroutines.
				if p := recover(); p != nil {
					slog.Error("tmdb pull: panic", "user_id", userID, "panic", p)
					job.finish(tmdbPullStateFailed, s.Now().UTC(), &TMDBPullError{Code: tmdbPullErrInternal, Message: tmdbPullInternalMessage})
				}
			}()
			s.runTMDBPull(jobCtx, userID, row.SessionID, row.AccountID, sections, job)
		}()
	}
	return s.TMDBLinkStatus(ctx, userID)
}

// SetTMDBSyncKinds writes the three ongoing switches (D-036). A switch turned
// off takes its pending rows with it, in the same transaction; one turned on
// queues nothing — the setup's Push is the one backfill. Allowed while
// broken: the broken_at guard still keeps the queue empty.
func (s *Service) SetTMDBSyncKinds(ctx context.Context, userID int64, k TMDBSyncKinds) (TMDBLinkStatus, error) {
	if _, err := getTMDBAccount(ctx, s.DB, userID); err != nil {
		return TMDBLinkStatus{}, err
	}
	err := s.DB.InTx(ctx, func(tx db.Tx) error {
		if err := updateTMDBSyncKinds(ctx, tx, userID, k); err != nil {
			return err
		}
		for kind, on := range map[string]bool{common.SyncKindRating: k.Ratings, common.SyncKindWatchlist: k.Watchlist, common.SyncKindFavorite: k.Favorites} {
			if on {
				continue
			}
			if err := deleteTMDBSyncKind(ctx, tx, userID, kind); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return TMDBLinkStatus{}, err
	}
	return s.TMDBLinkStatus(ctx, userID)
}

// tmdbSyncRunner is the worker's state: its WaitGroup, the wake-up channel
// writes kick after committing, and the two delays as fields so a test can
// shrink them.
type tmdbSyncRunner struct {
	wg        sync.WaitGroup
	kick      chan struct{}
	bootDelay time.Duration
	idlePoll  time.Duration
}

func newTMDBSyncRunner(kick chan struct{}) *tmdbSyncRunner {
	return &tmdbSyncRunner{kick: kick, bootDelay: tmdbSyncBootDelay, idlePoll: tmdbSyncIdlePoll}
}

// tmdbSyncLoop drains the outbox until ctx ends. Started by Run.
func (s *Service) tmdbSyncLoop(ctx context.Context) {
	timer := time.NewTimer(s.tmdbSync.bootDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-s.tmdbSync.kick:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		// A full batch means there may be more: loop until a short one, an
		// abort, or shutdown.
		for {
			rep := s.RunTMDBSyncBatch(ctx)
			if rep.Aborted || rep.Sent+rep.Deferred+rep.Dropped+rep.Broken < tmdbSyncBatch || ctx.Err() != nil {
				break
			}
		}
		timer.Reset(s.tmdbSync.idlePoll)
	}
}

// tmdbSyncReport is one batch's tally, for the log line and the tests.
type tmdbSyncReport struct {
	Sent     int // rows TMDB accepted, deleted
	Deferred int // rows that failed on an outage, scheduled again
	Dropped  int // rows TMDB or the catalog refused for good, deleted
	Broken   int // rows discarded because TMDB refused the user's session
	// Aborted is set when the batch stopped early — the breaker tripped or
	// the context ended — so the remaining rows wait for the next wake-up.
	Aborted bool
}

// RunTMDBSyncBatch sends one batch of due rows. Synchronous and
// report-returning so a test drives it without the loop; it never returns an
// error and recovers its own panics, because httpx.Recover guards handlers,
// not this goroutine.
//
// Per row: resolve the TMDB address locally, call TMDB, then settle. Success
// deletes the row; an outage defers it with backoff and counts against the
// batch's breaker (three in a row abandon the batch); a refused session marks
// that user's link broken and discards their rows — new ratings stop queueing
// until they link again; a 404 or a refused value is dropped, because retrying
// cannot change TMDB's answer. Every settle is guarded on updated_at, so a
// rating rewritten while its row was in flight survives to be sent again.
func (s *Service) RunTMDBSyncBatch(ctx context.Context) tmdbSyncReport {
	var rep tmdbSyncReport
	defer func() {
		if p := recover(); p != nil {
			slog.Error("tmdb sync: panic in a batch, continuing next wake-up", "panic", p)
			rep.Aborted = true
		}
	}()
	if s.TMDB == nil || !s.TMDB.Configured() {
		return rep
	}

	now := s.Now().UTC()
	rows, err := dueTMDBSync(ctx, s.DB, now.Format(time.RFC3339), tmdbSyncBatch)
	if err != nil {
		slog.Error("tmdb sync: listing due rows", "err", err)
		return rep
	}
	if len(rows) == 0 {
		return rep
	}

	var breaker common.ProviderBreaker
	brokenUsers := map[int64]bool{}
	for _, row := range rows {
		if ctx.Err() != nil || breaker.Open() {
			rep.Aborted = true
			break
		}
		if brokenUsers[row.UserID] {
			rep.Broken++
			continue
		}

		ids, err := resolveTMDBSyncTarget(ctx, s.DB, row.TargetType, row.TargetID)
		if err != nil {
			if errors.Is(err, httpx.ErrNotFound) {
				slog.Warn("tmdb sync: target no longer exists, dropping", "user_id", row.UserID, "target", row.TargetType, "id", row.TargetID)
				s.settleSync(ctx, row, &rep.Dropped, func(tx db.Tx) error { return settleTMDBSyncDone(ctx, tx, row.ID, row.UpdatedAt) })
				continue
			}
			slog.Error("tmdb sync: resolving target", "user_id", row.UserID, "target", row.TargetType, "id", row.TargetID, "err", err)
			continue
		}
		target := provider.RatingTarget{Kind: row.TargetType, TMDBID: ids.TMDBID, Season: ids.Season, Episode: ids.Episode}

		switch {
		case row.Kind == common.SyncKindWatchlist:
			err = s.TMDB.SetWatchlist(ctx, row.SessionID, row.AccountID, row.TargetType, ids.TMDBID, row.Op == "set")
		case row.Kind == common.SyncKindFavorite:
			err = s.TMDB.SetFavorite(ctx, row.SessionID, row.AccountID, row.TargetType, ids.TMDBID, row.Op == "set")
		case row.Op == "set" && row.Rating != nil:
			err = s.TMDB.SetRating(ctx, row.SessionID, target, *row.Rating)
		default:
			err = s.TMDB.DeleteRating(ctx, row.SessionID, target)
		}
		breaker.Observe(err)

		switch {
		case err == nil:
			s.settleSync(ctx, row, &rep.Sent, func(tx db.Tx) error { return settleTMDBSyncDone(ctx, tx, row.ID, row.UpdatedAt) })
		case errors.Is(err, provider.ErrSessionInvalid):
			slog.Warn("tmdb sync: tmdb refused the user's session, marking the link broken", "user_id", row.UserID)
			brokenUsers[row.UserID] = true
			at := s.Now().UTC().Format(time.RFC3339)
			s.settleSync(ctx, row, &rep.Broken, func(tx db.Tx) error {
				if err := markTMDBAccountBroken(ctx, tx, row.UserID, at); err != nil {
					return err
				}
				return deleteTMDBSyncForUser(ctx, tx, row.UserID)
			})
		case errors.Is(err, httpx.ErrNotFound), errors.Is(err, httpx.ErrValidation):
			slog.Warn("tmdb sync: tmdb refused the rating for good, dropping", "user_id", row.UserID, "target", row.TargetType, "id", row.TargetID, "err", err)
			s.settleSync(ctx, row, &rep.Dropped, func(tx db.Tx) error { return settleTMDBSyncDone(ctx, tx, row.ID, row.UpdatedAt) })
		default:
			attempts := row.Attempts + 1
			retryAt := s.Now().UTC().Add(tmdbSyncBackoff(attempts)).Format(time.RFC3339)
			slog.Warn("tmdb sync: send failed, will retry", "user_id", row.UserID, "target", row.TargetType, "id", row.TargetID, "attempt", attempts, "err", err)
			s.settleSync(ctx, row, &rep.Deferred, func(tx db.Tx) error {
				return deferTMDBSync(ctx, tx, row.ID, row.UpdatedAt, attempts, retryAt, err.Error())
			})
		}
	}
	if rep.Sent+rep.Deferred+rep.Dropped+rep.Broken > 0 {
		slog.Info("tmdb sync: batch finished", "sent", rep.Sent, "deferred", rep.Deferred, "dropped", rep.Dropped, "broken", rep.Broken, "aborted", rep.Aborted)
	}
	return rep
}

// settleSync runs one row's settle transaction and bumps the tally on
// success. A settle that fails is logged and left: the row stays due and the
// next batch sees it again.
func (s *Service) settleSync(ctx context.Context, row tmdbSyncRow, tally *int, fn func(tx db.Tx) error) {
	if err := s.DB.InTx(ctx, fn); err != nil {
		slog.Error("tmdb sync: settling row", "id", row.ID, "err", err)
		return
	}
	*tally++
}

// tmdbSyncBackoff is the delay before attempt n+1: base doubling per attempt,
// capped.
func tmdbSyncBackoff(attempts int) time.Duration {
	d := tmdbSyncRetryBase
	for i := 1; i < attempts && d < tmdbSyncRetryMax; i++ {
		d *= 2
	}
	if d > tmdbSyncRetryMax {
		d = tmdbSyncRetryMax
	}
	return d
}
