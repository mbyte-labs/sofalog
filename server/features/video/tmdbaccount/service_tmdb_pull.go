package tmdbaccount

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// The setup PULL (D-036): the one read of a user's TMDB account into the
// library, run once per link when the setup asks for it. It is the restore
// job's shape (service_restore.go) copied rather than shared — restoreJob is
// typed on the Ticket Box report — and it follows the restore's rules: every
// pulled item goes through the ordinary tracking gate (ResolveRestoreMovie /
// ResolveRestoreShow, D-015), is written in its own transaction, and the
// job lives in memory, one per user, forgotten by a restart. Two rules are
// its own. Pulled writes NEVER enqueue an outbox row — they came from TMDB,
// and sending them back would be a loop. And ratings OVERWRITE the local
// value (the user chose pull) while listings insert if absent, so re-running
// an interrupted pull converges.

const (
	tmdbPullStateRunning = "running"
	tmdbPullStateDone    = "done"
	tmdbPullStateFailed  = "failed"

	tmdbPullErrCancelled   = "cancelled"
	tmdbPullErrSession     = "session_invalid"
	tmdbPullErrUnavailable = "provider_unavailable"
	tmdbPullErrInternal    = "internal"

	tmdbPullCancelledMessage   = "the server stopped before the import finished; run the setup again"
	tmdbPullSessionMessage     = "TMDB no longer accepts the link; link again, then run the setup"
	tmdbPullUnavailableMessage = "TMDB could not be reached; run the setup again"
	tmdbPullInternalMessage    = "the import failed; run the setup again"

	tmdbPullSectionRatings   = "ratings"
	tmdbPullSectionWatchlist = "watchlist"
	tmdbPullSectionFavorites = "favorites"

	// tmdbPullMaxPages mirrors TMDB's own paging ceiling (provider.tmdbMaxPage):
	// a list the account read says is longer is cut there, never looped past.
	tmdbPullMaxPages = 500
)

// TMDBPullJob is one user's setup pull: its state, where it is, and what it
// has done. Rides TMDBLinkStatus.pull; the report is partial while running.
type TMDBPullJob struct {
	State      string           `json:"state"` // running | done | failed
	StartedAt  string           `json:"started_at"`
	FinishedAt *string          `json:"finished_at"`
	Progress   TMDBPullProgress `json:"progress"`
	Report     TMDBPullReport   `json:"report"`
	Error      *TMDBPullError   `json:"error"`
}

type TMDBPullProgress struct {
	Section string `json:"section"` // ratings | watchlist | favorites; "" once finished
	List    string `json:"list"`    // movies | shows | episodes
	Page    int    `json:"page"`
	// TotalPages is 0 until the first page of the current list answers.
	TotalPages int `json:"total_pages"`
	ItemsDone  int `json:"items_done"`
}

type TMDBPullReport struct {
	RatingsImported        int `json:"ratings_imported"` // movies and shows
	EpisodeRatingsImported int `json:"episode_ratings_imported"`
	WatchlistAdded         int `json:"watchlist_added"`
	FavoritesAdded         int `json:"favorites_added"`
	MoviesMaterialised     int `json:"movies_materialised"`
	ShowsMaterialised      int `json:"shows_materialised"`
	SkippedTotal           int `json:"skipped_total"`
	// SkippedByReason uses the restore's reason codes (SkipMovieNotFound,
	// SkipEpisodeNotFound, SkipProviderUnavailable, …).
	SkippedByReason map[string]int `json:"skipped_by_reason"`
}

type TMDBPullError struct {
	Code    string `json:"code"` // cancelled | session_invalid | provider_unavailable | internal
	Message string `json:"message"`
}

// tmdbPullSections is which kinds the setup asked to pull.
type tmdbPullSections struct {
	Ratings   bool
	Watchlist bool
	Favorites bool
}

type tmdbPullRunner struct {
	mu   sync.Mutex
	jobs map[int64]*tmdbPullJob
	wg   sync.WaitGroup
}

func newTMDBPullRunner() *tmdbPullRunner {
	return &tmdbPullRunner{jobs: map[int64]*tmdbPullJob{}}
}

// start registers a job for the user, refusing while one is still running.
func (r *tmdbPullRunner) start(userID int64, job *tmdbPullJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.jobs[userID]; ok && existing.running() {
		return fmt.Errorf("%w: a TMDB import is already running for this account", httpx.ErrConflict)
	}
	r.jobs[userID] = job
	return nil
}

// get is nil-safe: a Service built by hand in a test may have no runner.
func (r *tmdbPullRunner) get(userID int64) (*tmdbPullJob, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[userID]
	return job, ok
}

// cancel stops this user's job if one is running. Nil-safe like get.
func (r *tmdbPullRunner) cancel(userID int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if job, ok := r.jobs[userID]; ok && job.cancel != nil {
		job.cancel()
	}
}

func (r *tmdbPullRunner) cancelAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range r.jobs {
		if job.cancel != nil {
			job.cancel()
		}
	}
}

// tmdbPullJob is the mutable record behind one TMDBPullJob snapshot. Every
// write goes through the mutex; readers get a deep copy.
type tmdbPullJob struct {
	mu     sync.Mutex
	snap   TMDBPullJob
	cancel context.CancelFunc
}

func newTMDBPullJob(startedAt time.Time) *tmdbPullJob {
	return &tmdbPullJob{snap: TMDBPullJob{
		State:     tmdbPullStateRunning,
		StartedAt: startedAt.Format(time.RFC3339),
		Report:    TMDBPullReport{SkippedByReason: map[string]int{}},
	}}
}

func (j *tmdbPullJob) update(fn func(*TMDBPullJob)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.snap)
}

// setProgress names the list being read. A zero total keeps the previous
// one: the first page's answer is what fills it in.
func (j *tmdbPullJob) setProgress(section, list string, page, totalPages int) {
	j.update(func(s *TMDBPullJob) {
		if s.Progress.Section != section || s.Progress.List != list {
			s.Progress.TotalPages = 0
		}
		s.Progress.Section = section
		s.Progress.List = list
		s.Progress.Page = page
		if totalPages > 0 {
			s.Progress.TotalPages = totalPages
		}
	})
}

func (j *tmdbPullJob) itemDone() {
	j.update(func(s *TMDBPullJob) { s.Progress.ItemsDone++ })
}

func (j *tmdbPullJob) skip(reason string) {
	j.update(func(s *TMDBPullJob) {
		s.Report.SkippedTotal++
		s.Report.SkippedByReason[reason]++
	})
}

func (j *tmdbPullJob) finish(state string, at time.Time, jobErr *TMDBPullError) {
	j.update(func(s *TMDBPullJob) {
		s.State = state
		finished := at.Format(time.RFC3339)
		s.FinishedAt = &finished
		s.Progress.Section = ""
		s.Progress.List = ""
		s.Error = jobErr
	})
}

func (j *tmdbPullJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap.State == tmdbPullStateRunning
}

// snapshot deep-copies the record so a caller encoding it races with nothing.
func (j *tmdbPullJob) snapshot() TMDBPullJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.snap
	out.Report.SkippedByReason = make(map[string]int, len(j.snap.Report.SkippedByReason))
	for k, v := range j.snap.Report.SkippedByReason {
		out.Report.SkippedByReason[k] = v
	}
	if j.snap.FinishedAt != nil {
		v := *j.snap.FinishedAt
		out.FinishedAt = &v
	}
	if j.snap.Error != nil {
		e := *j.snap.Error
		out.Error = &e
	}
	return out
}

// roundTMDBRating maps TMDB's 0.5-step value onto sofalog's 1..10 integer:
// half up, clamped — 0.5 → 1, 7.5 → 8, 10 → 10.
func roundTMDBRating(v float64) int {
	n := int(math.Floor(v + 0.5))
	if n < 1 {
		return 1
	}
	if n > 10 {
		return 10
	}
	return n
}

// runTMDBPull does the work and never returns an error: everything it cannot
// absorb ends the job as failed with the reason on the record. Synchronous so
// a test can drive it without the runner. Sections run ratings → watchlist →
// favorites, each list page by page; a list that cannot be read at all ends
// the job (a refused session also breaks the link, as the worker would),
// while one item that cannot be resolved is a skip. The setup is marked done
// only at the very end, so a failure leaves it to be run again.
func (s *Service) runTMDBPull(ctx context.Context, userID int64, sessionID string, accountID int64, sections tmdbPullSections, job *tmdbPullJob) {
	var breaker common.ProviderBreaker
	fail := func(code, message string) {
		job.finish(tmdbPullStateFailed, s.Now().UTC(), &TMDBPullError{Code: code, Message: message})
	}
	// run reads one whole list and reports whether the job may go on.
	run := func(section, list string, read func(page int) (int, error)) bool {
		err := s.pullPaged(ctx, job, section, list, read)
		switch {
		case err == nil:
			return true
		case ctx.Err() != nil:
			fail(tmdbPullErrCancelled, tmdbPullCancelledMessage)
		case errors.Is(err, provider.ErrSessionInvalid):
			slog.Warn("tmdb pull: tmdb refused the user's session, marking the link broken", "user_id", userID)
			at := s.Now().UTC().Format(time.RFC3339)
			if err := s.DB.InTx(ctx, func(tx db.Tx) error {
				if err := markTMDBAccountBroken(ctx, tx, userID, at); err != nil {
					return err
				}
				return deleteTMDBSyncForUser(ctx, tx, userID)
			}); err != nil {
				slog.Error("tmdb pull: marking the link broken", "user_id", userID, "err", err)
			}
			fail(tmdbPullErrSession, tmdbPullSessionMessage)
		case errors.Is(err, httpx.ErrProviderUnavailable):
			slog.Warn("tmdb pull: list read failed", "user_id", userID, "section", section, "list", list, "err", err)
			fail(tmdbPullErrUnavailable, tmdbPullUnavailableMessage)
		default:
			slog.Error("tmdb pull: "+section+" "+list, "user_id", userID, "err", err)
			fail(tmdbPullErrInternal, tmdbPullInternalMessage)
		}
		return false
	}

	if sections.Ratings {
		ok := run(tmdbPullSectionRatings, "movies", func(page int) (int, error) {
			pg, err := s.TMDB.AccountRatedMovies(ctx, sessionID, accountID, page)
			if err != nil {
				return 0, err
			}
			job.setProgress(tmdbPullSectionRatings, "movies", page, pg.TotalPages)
			for _, it := range pg.Results {
				if err := s.pullRating(ctx, userID, "movie", it.ID, it.Rating, &breaker, job); err != nil {
					return 0, err
				}
				job.itemDone()
			}
			return pg.TotalPages, nil
		})
		if !ok {
			return
		}
		ok = run(tmdbPullSectionRatings, "shows", func(page int) (int, error) {
			pg, err := s.TMDB.AccountRatedShows(ctx, sessionID, accountID, page)
			if err != nil {
				return 0, err
			}
			job.setProgress(tmdbPullSectionRatings, "shows", page, pg.TotalPages)
			for _, it := range pg.Results {
				if err := s.pullRating(ctx, userID, "show", it.ID, it.Rating, &breaker, job); err != nil {
					return 0, err
				}
				job.itemDone()
			}
			return pg.TotalPages, nil
		})
		if !ok {
			return
		}
		ok = run(tmdbPullSectionRatings, "episodes", func(page int) (int, error) {
			pg, err := s.TMDB.AccountRatedEpisodes(ctx, sessionID, accountID, page)
			if err != nil {
				return 0, err
			}
			job.setProgress(tmdbPullSectionRatings, "episodes", page, pg.TotalPages)
			for _, it := range pg.Results {
				if err := s.pullEpisodeRating(ctx, userID, it.ShowID, it.Season, it.Episode, it.Rating, &breaker, job); err != nil {
					return 0, err
				}
				job.itemDone()
			}
			return pg.TotalPages, nil
		})
		if !ok {
			return
		}
	}

	// The watchlist and the favorites are the same two lists with a different
	// kind, so one loop over the two sections and the two target types.
	listed := []struct {
		on      bool
		section string
		kind    string
		read    func(ctx context.Context, sessionID string, accountID int64, kind string, page int) (provider.TMDBListedPage, error)
	}{
		{sections.Watchlist, tmdbPullSectionWatchlist, common.SyncKindWatchlist, s.tmdbAccountWatchlist},
		{sections.Favorites, tmdbPullSectionFavorites, common.SyncKindFavorite, s.tmdbAccountFavorites},
	}
	for _, l := range listed {
		if !l.on {
			continue
		}
		for _, targetType := range []string{"movie", "show"} {
			list := targetType + "s"
			ok := run(l.section, list, func(page int) (int, error) {
				pg, err := l.read(ctx, sessionID, accountID, targetType, page)
				if err != nil {
					return 0, err
				}
				job.setProgress(l.section, list, page, pg.TotalPages)
				for _, it := range pg.Results {
					if err := s.pullListed(ctx, userID, l.kind, targetType, it.ID, &breaker, job); err != nil {
						return 0, err
					}
					job.itemDone()
				}
				return pg.TotalPages, nil
			})
			if !ok {
				return
			}
		}
	}

	if ctx.Err() != nil {
		fail(tmdbPullErrCancelled, tmdbPullCancelledMessage)
		return
	}
	if err := s.DB.InTx(ctx, func(tx db.Tx) error { return setTMDBSetupDone(ctx, tx, userID) }); err != nil {
		slog.Error("tmdb pull: marking the setup done", "user_id", userID, "err", err)
		fail(tmdbPullErrInternal, tmdbPullInternalMessage)
		return
	}
	snap := job.snapshot()
	slog.Info("tmdb pull: finished", "user_id", userID, "ratings", snap.Report.RatingsImported, "episode_ratings", snap.Report.EpisodeRatingsImported,
		"watchlist", snap.Report.WatchlistAdded, "favorites", snap.Report.FavoritesAdded, "skipped", snap.Report.SkippedTotal)
	job.finish(tmdbPullStateDone, s.Now().UTC(), nil)
}

// The two list readers as method values with one shape, so runTMDBPull can
// table them.
func (s *Service) tmdbAccountWatchlist(ctx context.Context, sessionID string, accountID int64, kind string, page int) (provider.TMDBListedPage, error) {
	return s.TMDB.AccountWatchlist(ctx, sessionID, accountID, kind, page)
}

func (s *Service) tmdbAccountFavorites(ctx context.Context, sessionID string, accountID int64, kind string, page int) (provider.TMDBListedPage, error) {
	return s.TMDB.AccountFavorites(ctx, sessionID, accountID, kind, page)
}

// pullPaged walks one list from page 1 to the total the first page reports,
// so the paging loop exists once. read fetches and handles one page and
// returns the total page count.
func (s *Service) pullPaged(ctx context.Context, job *tmdbPullJob, section, list string, read func(page int) (int, error)) error {
	for page := 1; page <= tmdbPullMaxPages; page++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		job.setProgress(section, list, page, 0)
		total, err := read(page)
		if err != nil {
			return err
		}
		if page >= total {
			return nil
		}
	}
	return nil
}

// pullRating writes one rated movie or show: resolve (and materialise)
// outside any transaction, then one transaction for the upsert. A resolution
// failure the run can absorb is a skip; anything else ends the job.
func (s *Service) pullRating(ctx context.Context, userID int64, targetType string, tmdbID int64, value float64, breaker *common.ProviderBreaker, job *tmdbPullJob) error {
	targetID, ok, err := s.pullResolve(ctx, targetType, tmdbID, breaker, job)
	if err != nil || !ok {
		return err
	}
	at := s.Now().UTC().Format(time.RFC3339)
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		return common.UpsertVideoRating(ctx, tx, userID, targetType, targetID, roundTMDBRating(value), at)
	})
	if err != nil {
		return err
	}
	job.update(func(j *TMDBPullJob) { j.Report.RatingsImported++ })
	return nil
}

// pullEpisodeRating is pullRating for an episode, addressed by its show's
// TMDB id and its position — the catalog's own address. An episode the show
// does not have (TMDB renumbered, or an unaired one the snapshot lacks) is
// a skip.
func (s *Service) pullEpisodeRating(ctx context.Context, userID, showTMDBID int64, season, episode int, value float64, breaker *common.ProviderBreaker, job *tmdbPullJob) error {
	showID, ok, err := s.pullResolve(ctx, "show", showTMDBID, breaker, job)
	if err != nil || !ok {
		return err
	}
	ep, err := common.GetEpisodeByShowKey(ctx, s.DB, showID, season, episode)
	if errors.Is(err, httpx.ErrNotFound) {
		job.skip(common.SkipEpisodeNotFound)
		return nil
	}
	if err != nil {
		return err
	}
	at := s.Now().UTC().Format(time.RFC3339)
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		return common.UpsertVideoRating(ctx, tx, userID, "episode", ep.ID, roundTMDBRating(value), at)
	})
	if err != nil {
		return err
	}
	job.update(func(j *TMDBPullJob) { j.Report.EpisodeRatingsImported++ })
	return nil
}

// pullListed adds one watchlist or favorite entry, insert-if-absent: an entry
// already held counts for nothing, so a re-run converges.
func (s *Service) pullListed(ctx context.Context, userID int64, kind, targetType string, tmdbID int64, breaker *common.ProviderBreaker, job *tmdbPullJob) error {
	targetID, ok, err := s.pullResolve(ctx, targetType, tmdbID, breaker, job)
	if err != nil || !ok {
		return err
	}
	at := s.Now().UTC().Format(time.RFC3339)
	var created bool
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		var err error
		if kind == common.SyncKindFavorite {
			created, err = common.InsertFavorite(ctx, tx, userID, targetType, targetID, at)
		} else {
			created, err = common.InsertWatchlist(ctx, tx, userID, targetType, targetID, at)
		}
		return err
	})
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	job.update(func(j *TMDBPullJob) {
		if kind == common.SyncKindFavorite {
			j.Report.FavoritesAdded++
		} else {
			j.Report.WatchlistAdded++
		}
	})
	return nil
}

// pullResolve finds the local row for a movie or show by TMDB id,
// materialising it through the tracking gate when the library has never
// seen it (and then enqueueing its artwork, as tracking does). Returns the
// local id and whether the caller may go on: a skip has been recorded when
// not, and a returned error is one the run cannot absorb.
func (s *Service) pullResolve(ctx context.Context, targetType string, tmdbID int64, breaker *common.ProviderBreaker, job *tmdbPullJob) (int64, bool, error) {
	var (
		id           int64
		materialised bool
		err          error
		notFound     = common.SkipMovieNotFound
		unavailable  = common.SkipMovieUnavailable
	)
	switch targetType {
	case "movie":
		var row common.MovieRow
		row, materialised, err = s.ResolveRestoreMovie(ctx, tmdbID, breaker)
		if err == nil {
			id = row.ID
			if materialised {
				s.SyncMovieAssets(ctx, row)
			}
		}
	case "show":
		notFound, unavailable = common.SkipShowNotFound, common.SkipShowUnavailable
		var row common.ShowRow
		row, materialised, err = s.ResolveRestoreShow(ctx, tmdbID, breaker)
		if err == nil {
			id = row.ID
			if materialised {
				s.SyncShowAssets(ctx, row)
			}
		}
	default:
		return 0, false, fmt.Errorf("%w: %q is not a pull target", httpx.ErrValidation, targetType)
	}
	if err != nil {
		reason, ok := common.GroupSkipReason(err, notFound, unavailable)
		if !ok {
			return 0, false, err
		}
		job.skip(reason)
		return 0, false, nil
	}
	if materialised {
		job.update(func(j *TMDBPullJob) {
			if targetType == "movie" {
				j.Report.MoviesMaterialised++
			} else {
				j.Report.ShowsMaterialised++
			}
		})
	}
	return id, true, nil
}
