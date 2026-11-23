package refresh

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/tz"
)

// The scheduled TMDB refresh — SPEC §5's fourth provider call site, D-006's one
// scheduler, and D-025's two jobs riding it.
//
// Two jobs, each with its own interval, on ONE hourly loop:
//
//   - ACTIVE: watchlisted movies and shows, plus followed shows still in
//     production. Weekly by default — this is what feeds the calendar.
//   - ARCHIVE: watched movies and followed shows TMDB marks Ended/Canceled,
//     neither watchlisted. Quarterly by default — only artwork and streaming
//     listings still move.
//
// There is no "last run" state anywhere: a pass picks the items of its class
// whose fetched_at predates now minus the job's interval, so a restart or a
// long downtime self-heals on the first tick, and a page view that refreshed an
// item on its own simply postpones it. The row timestamp IS the schedule.
//
// Every tick ends with the status sweep, which is local and runs whatever the
// intervals say: D-010's "whole-table pass". Eligibility depends on today's
// date, so a show can go stale with no write anywhere — midnight passes and an
// episode becomes aired — and the sweep is what bounds that drift to one hour.

const (
	// refreshBootDelay is how long after start the first tick runs — enough for
	// the process to settle, short enough that a server that was down for a
	// week catches up promptly.
	refreshBootDelay = time.Minute
	// refreshTick is how often the loop checks for due items. The intervals
	// are configuration; this is not — it only bounds how late a due item can
	// be, and the config floor (1h) matches it.
	refreshTick = time.Hour
)

// refreshRunner is the scheduler's state: the two intervals, the loop's
// WaitGroup, and the 404 memory (see suppress).
type refreshRunner struct {
	active  time.Duration
	archive time.Duration

	// bootDelay and tick are fields rather than the constants directly so a
	// test can drive the loop without waiting an hour.
	bootDelay time.Duration
	tick      time.Duration

	wg sync.WaitGroup

	// notFound remembers items TMDB answered 404 for, with the instant the
	// memory expires. fetched_at never advances on a failure, so without this
	// a title TMDB removed would be retried — one call and one log line — on
	// every tick for good. One interval's silence is the compromise: TMDB
	// occasionally does restore a removed entry.
	mu       sync.Mutex
	notFound map[string]time.Time
}

func newRefreshRunner(active, archive time.Duration) *refreshRunner {
	return &refreshRunner{
		active: active, archive: archive,
		bootDelay: refreshBootDelay, tick: refreshTick,
		notFound: map[string]time.Time{},
	}
}

func (r *refreshRunner) suppress(key string, until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notFound[key] = until
}

// suppressed reports whether key is still under a 404 memory, dropping an
// expired one on the way.
func (r *refreshRunner) suppressed(key string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.notFound[key]
	if !ok {
		return false
	}
	if !now.Before(until) {
		delete(r.notFound, key)
		return false
	}
	return true
}

// refreshReport is one pass's tally, for the log line and the tests.
type refreshReport struct {
	Class   refreshClass
	Movies  int // movie snapshots rewritten
	Shows   int // show roots rewritten
	Seasons int // open seasons rewritten
	Failed  int // fetches that failed (the snapshot stays)
	Skipped int // items under a 404 memory
	// Aborted is set when the pass stopped early — the breaker tripped or the
	// context ended — so the remaining items wait for the next tick.
	Aborted bool
}

// refreshLoop ticks until ctx ends. Started by Run.
func (s *Service) refreshLoop(ctx context.Context) {
	timer := time.NewTimer(s.refresh.bootDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.runRefreshTick(ctx)
		timer.Reset(s.refresh.tick)
	}
}

// runRefreshTick is one iteration: the two fetch passes, then the sweep. It
// never returns an error — everything is logged and retried next tick — and it
// recovers its own panics, because httpx.Recover guards handlers, not this
// goroutine, and one bad item must not end the schedule for the process's life.
func (s *Service) runRefreshTick(ctx context.Context) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("refresh: panic in a tick, continuing next tick", "panic", p)
		}
	}()

	if s.TMDB != nil && s.TMDB.Configured() {
		var breaker common.ProviderBreaker
		jobs := []struct {
			class refreshClass
			every time.Duration
		}{{RefreshActive, s.refresh.active}, {RefreshArchive, s.refresh.archive}}
		for _, job := range jobs {
			if job.every == 0 || ctx.Err() != nil || breaker.Open() {
				continue
			}
			rep := s.runRefreshPass(ctx, job.class, job.every, &breaker)
			slog.Info("refresh: pass finished", "class", rep.Class,
				"movies", rep.Movies, "shows", rep.Shows, "seasons", rep.Seasons,
				"failed", rep.Failed, "skipped", rep.Skipped, "aborted", rep.Aborted)
		}
	} else if s.refresh.active != 0 || s.refresh.archive != 0 {
		slog.Debug("refresh: tmdb not configured, nothing fetched")
	}

	if ctx.Err() != nil {
		return
	}
	n, err := s.runStatusSweep(ctx)
	if err != nil {
		slog.Warn("refresh: status sweep stopped early", "recomputed", n, "err", err)
		return
	}
	slog.Info("refresh: status sweep finished", "recomputed", n)
}

// runRefreshPass refreshes every due item of one class, sequentially, sharing
// the tick's breaker. Synchronous and report-returning so a test drives it
// without the loop.
//
// Per movie it is the details view's own pair: RefreshMovie then the asset
// reconcile. Per show it is RefreshShow (root + any season TMDB has added since)
// followed by the show's open seasons — computed AFTER the root refresh, so a
// season the root just materialised, with a fresh fetched_at, is not fetched
// twice. The active class also takes every followed show holding an episode
// that aired since its season's snapshot, whatever the show's age, and
// refetches that season with the open ones. RefreshSeason reconciles the show's assets itself, so the extra sync
// runs only when no season did. No fetch ever sits inside a transaction
// (writeShow's WAL note), and no status is recomputed here: the sweep at the
// end of the tick covers every show at once.
func (s *Service) runRefreshPass(ctx context.Context, class refreshClass, every time.Duration, breaker *common.ProviderBreaker) refreshReport {
	rep := refreshReport{Class: class}
	now := s.Now().UTC()
	cutoff := now.Add(-every).Format(time.RFC3339)
	// No user here: "open season" is a catalog question, asked in the
	// server's default zone (D-038).
	today := tz.Day(now, s.DefaultLocation())

	// fail records one failed fetch. A 404 means TMDB no longer lists the
	// item: the snapshot stays as it is (never a delete — the log may reference
	// it) and the item is left alone for one interval.
	fail := func(key string, err error) {
		rep.Failed++
		if errors.Is(err, httpx.ErrNotFound) {
			s.refresh.suppress(key, now.Add(every))
			slog.Warn("refresh: tmdb no longer lists the item, keeping the snapshot", "class", class, "item", key)
			return
		}
		slog.Warn("refresh: fetch failed, keeping the stale snapshot", "class", class, "item", key, "err", err)
	}
	// stop reports whether the pass must end now: the context is gone or TMDB
	// has failed ProviderBreakerTrips times in a row.
	stop := func() bool {
		if ctx.Err() != nil || breaker.Open() {
			rep.Aborted = true
			return true
		}
		return false
	}

	movies, err := DueMovies(ctx, s.DB, class, cutoff)
	if err != nil {
		slog.Error("refresh: listing due movies", "class", class, "err", err)
		return rep
	}
	for _, m := range movies {
		if stop() {
			return rep
		}
		key := fmt.Sprintf("movie %d", m.tmdbID)
		if s.refresh.suppressed(key, now) {
			rep.Skipped++
			continue
		}
		row, err := s.RefreshMovie(ctx, m.tmdbID)
		breaker.Observe(err)
		if err != nil {
			fail(key, err)
			continue
		}
		s.SyncMovieAssets(ctx, row)
		rep.Movies++
	}

	shows, err := DueShows(ctx, s.DB, class, cutoff)
	if err != nil {
		slog.Error("refresh: listing due shows", "class", class, "err", err)
		return rep
	}
	work := make([]int64, 0, len(shows))
	listed := make(map[int64]bool, len(shows))
	for _, sh := range shows {
		work = append(work, sh.TMDBID)
		listed[sh.ID] = true
	}
	// The active job's one arm that is not a cutoff: a followed show whose
	// episode aired since its season was fetched joins the pass whatever its
	// age, and that season is refetched with it. A failure here costs only
	// the arm — the cutoff's own list still runs.
	airedSeasons := map[int64][]int{}
	if class == RefreshActive {
		since := tz.Day(now.Add(-every), s.DefaultLocation())
		recent, err := recentlyAiredSeasons(ctx, s.DB, since, today)
		if err != nil {
			slog.Error("refresh: listing recently aired seasons", "err", err)
		}
		for _, a := range recent {
			if !airedSinceFetch(a.fetchedAt, a.lastAired, s.DefaultLocation()) {
				continue
			}
			airedSeasons[a.showID] = append(airedSeasons[a.showID], a.season)
			if !listed[a.showID] {
				listed[a.showID] = true
				work = append(work, a.showTMDBID)
			}
		}
	}
	for _, tmdbID := range work {
		if stop() {
			return rep
		}
		key := fmt.Sprintf("show %d", tmdbID)
		if s.refresh.suppressed(key, now) {
			rep.Skipped++
			continue
		}
		fresh, err := s.RefreshShow(ctx, tmdbID)
		breaker.Observe(err)
		if err != nil {
			// A root that will not fetch takes its seasons with it: they would
			// be fetched against a stale root, and an outage would count
			// several times over against the breaker.
			fail(key, err)
			continue
		}
		rep.Shows++

		open, err := dueOpenSeasons(ctx, s.DB, fresh.ID, cutoff, today)
		if err != nil {
			slog.Error("refresh: listing open seasons", "show_id", fresh.ID, "err", err)
			s.SyncShowAssets(ctx, fresh)
			continue
		}
		if extra := airedSeasons[fresh.ID]; len(extra) > 0 {
			open = append(open, extra...)
			slices.Sort(open)
			open = slices.Compact(open)
		}
		refreshed := 0
		for _, n := range open {
			if stop() {
				break
			}
			seasonKey := fmt.Sprintf("show %d season %d", tmdbID, n)
			if s.refresh.suppressed(seasonKey, now) {
				rep.Skipped++
				continue
			}
			_, err := s.RefreshSeason(ctx, fresh, n)
			breaker.Observe(err)
			if err != nil {
				fail(seasonKey, err)
				continue
			}
			refreshed++
		}
		rep.Seasons += refreshed
		if refreshed == 0 {
			s.SyncShowAssets(ctx, fresh)
		}
		if rep.Aborted {
			return rep
		}
	}
	return rep
}

// airedSinceFetch reports whether an episode dated lastAired (YYYY-MM-DD)
// aired after the snapshot taken at fetchedAt: the snapshot's day, read in
// loc, is strictly before the air date. A snapshot taken ON the air day
// counts as current, which is also what ends the arm — one refetch moves
// fetched_at to today. An unreadable instant counts as stale.
func airedSinceFetch(fetchedAt, lastAired string, loc *time.Location) bool {
	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return true
	}
	return tz.Day(t, loc) < lastAired
}

// runStatusSweep recomputes every followed show's status for every follower —
// the unscoped whole-table pass D-010 promises, done as a loop over the scoped
// single writer so there is exactly one recompute in the codebase. The clock
// is read once: the sweep is seconds long, and every row should agree on the
// instant. Where "aired" ends is each user's own midnight (D-038), so today is
// that instant's day in the pair's zone. One transaction per pair keeps the
// write lock short. It
// returns how many pairs it recomputed and the error that stopped it, if any.
func (s *Service) runStatusSweep(ctx context.Context) (int, error) {
	targets, err := showStatusTargets(ctx, s.DB)
	if err != nil {
		return 0, err
	}
	at := s.Now()
	done := 0
	for _, t := range targets {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		clk := s.ClockIn(at, t.zone)
		err := s.DB.InTx(ctx, func(tx db.Tx) error {
			return common.RecomputeShowStatus(ctx, tx, t.userID, t.showID, clk.Today, clk.Now)
		})
		if err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
