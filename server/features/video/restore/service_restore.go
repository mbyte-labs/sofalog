package restore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

const (
	ticketBoxSchemaVersion = 5
)

const (
	// restoreSkipCap bounds the skipped list the job keeps and the report
	// returns. The per-reason counts and the total always cover everything.
	restoreSkipCap = 200
)

// TicketBoxDump is the export file, field names kept as Ticket Box writes them.
type TicketBoxDump struct {
	SchemaVersion int                       `json:"schemaVersion"`
	ExportedAt    string                    `json:"exportedAt"`
	History       []TicketBoxHistoryEntry   `json:"history"`
	Ratings       []TicketBoxRatingEntry    `json:"ratings"`
	Watchlist     []TicketBoxWatchlistEntry `json:"watchlist"`
	Favorites     []TicketBoxFavoriteEntry  `json:"favorites"`
}

// TicketBoxAddress is the identity fields every entry kind shares. TMDBID and
// IMDBID are the ITEM's own — for a season or an episode, its own, which
// sofalog has no use for; episodes are addressed by ShowTMDBID + Season +
// Episode. IMDBID is never read: it is declared so the strict decode accepts it.
type TicketBoxAddress struct {
	Type       string  `json:"type"`
	TMDBID     *int64  `json:"tmdbId"`
	IMDBID     *string `json:"imdbId"`
	ShowTMDBID *int64  `json:"showTmdbId"`
	Season     *int    `json:"season"`
	Episode    *int    `json:"episode"`
}

type TicketBoxHistoryEntry struct {
	TicketBoxAddress
	AddedAt     string `json:"addedAt"`
	HistoryType string `json:"historyType"`
}

type TicketBoxRatingEntry struct {
	TicketBoxAddress
	Rating  *int   `json:"rating"`
	RatedAt string `json:"ratedAt"`
}

type TicketBoxWatchlistEntry struct {
	TicketBoxAddress
	ListedAt string `json:"listedAt"`
}

type TicketBoxFavoriteEntry struct {
	TicketBoxAddress
	FavoritedAt string `json:"favoritedAt"`
}

// RestoreJob is one user's restore: its state, how far along it is, and what
// it has done so far. The report is partial while running and final after.
type RestoreJob struct {
	Kind       string          `json:"kind"`  // ticket_box
	State      string          `json:"state"` // running | done | failed
	StartedAt  string          `json:"started_at"`
	FinishedAt *string         `json:"finished_at"`
	Progress   RestoreProgress `json:"progress"`
	Report     RestoreReport   `json:"report"`
	Error      *RestoreError   `json:"error"`
}

type RestoreProgress struct {
	// A group is one movie or one show: the unit of work, and of the
	// transaction. Counted after validation, so the total is known up front.
	GroupsTotal int             `json:"groups_total"`
	GroupsDone  int             `json:"groups_done"`
	Current     *RestoreCurrent `json:"current"`
}

type RestoreCurrent struct {
	Type   string  `json:"type"` // movie | show
	TMDBID int64   `json:"tmdb_id"`
	Title  *string `json:"title"`
}

type RestoreReport struct {
	MoviesTotal        int            `json:"movies_total"`
	ShowsTotal         int            `json:"shows_total"`
	MoviesMaterialised int            `json:"movies_materialised"`
	ShowsMaterialised  int            `json:"shows_materialised"`
	HistoryInserted    int            `json:"history_inserted"`
	HistoryReplaced    int            `json:"history_replaced"`
	RatingsUpserted    int            `json:"ratings_upserted"`
	WatchlistInserted  int            `json:"watchlist_inserted"`
	FavoritesInserted  int            `json:"favorites_inserted"`
	SkippedTotal       int            `json:"skipped_total"`
	SkippedByReason    map[string]int `json:"skipped_by_reason"`
	Skipped            []RestoreSkip  `json:"skipped"` // the first restoreSkipCap only
}

// RestoreSkip names one entry the restore did not take and why. Reason is a
// stable code the SPA maps to words; Hint narrows an episode_not_found to its
// likely cause, again as a code — the SPA owns every sentence.
type RestoreSkip struct {
	Kind       string `json:"kind"` // history | rating | watchlist | favorite
	Type       string `json:"type"` // the entry's type as the file gave it
	TMDBID     *int64 `json:"tmdb_id"`
	ShowTMDBID *int64 `json:"show_tmdb_id"`
	Season     *int   `json:"season"`
	Episode    *int   `json:"episode"`
	Reason     string `json:"reason"`
	// Title is the movie's — for a show, season or episode entry, the show's —
	// title when the catalog holds it, filled when the job finishes
	// (enrichRestoreSkips). Null for an item TMDB never gave sofalog.
	Title *string `json:"title"`
	// Hint is set on episode_not_found only: specials_numbering |
	// season_missing | past_season_end | numbering_gap (episodeSkipHint).
	Hint *string `json:"hint"`
	// TMDBLastEpisode is TMDB's highest episode number in the entry's season,
	// beside the two hints it explains.
	TMDBLastEpisode *int `json:"tmdb_last_episode"`
}

type RestoreError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Skip reasons. Validation reasons come from the file alone; the rest arise
// while running, against TMDB or the catalog.
const (
	skipMissingTMDBID = "missing_tmdb_id"
)

const (
	skipUnknownType = "unknown_type"
)

const (
	skipMissingPosition = "missing_position"
)

const (
	skipInvalidTimestamp = "invalid_timestamp"
)

const (
	skipFutureTimestamp = "future_timestamp"
)

const (
	skipInvalidWatchType = "invalid_watch_type"
)

const (
	skipRatingOutOfRange = "rating_out_of_range"
)

const (
	skipSeasonRating = "season_rating_unsupported"
)

const (
	skipWatchlistTarget = "watchlist_target_unsupported"
)

const (
	hintSpecialsNumbering = "specials_numbering"
)

const (
	hintSeasonMissing = "season_missing"
)

const (
	hintPastSeasonEnd = "past_season_end"
)

const (
	hintNumberingGap = "numbering_gap"
)

const (
	restoreErrCancelled = "cancelled"
)

const (
	restoreErrInternal = "internal"
)

const (
	restoreStateRunning = "running"
)

const (
	restoreStateDone = "done"
)

const (
	restoreStateFailed = "failed"
)

const (
	restoreKindTicketBox = "ticket_box"
)

const (
	restoreGroupMovie = "movie"
)

const (
	restoreGroupShow = "show"
)

const (
	restoreKindHistory = "history"
)

const (
	restoreKindRating = "rating"
)

const (
	restoreKindWatchlist = "watchlist"
)

const (
	restoreKindFavorite = "favorite"
)

const (
	restoreInternalErrMessage = "the import stopped on an internal error; the server log has the cause"
)

const (
	restoreCancelledErrMessage = "the server shut down before the import finished; run it again"
)

// watchTypes is the provenance vocabulary user_watch_history accepts — the
// CHECK's set, stated once so the file's historyType is validated here rather
// than failing a whole transaction on the constraint.
var watchTypes = []string{"watch", "checkin", "scrobble"}

// parseTicketBoxDump reads the file in two passes. The version probe comes
// FIRST and is lenient, because the useful answer to a version-1 file is "not
// supported, export again with version 2" — a strict decode would instead
// fail on the first unknown field and say nothing about why. The second pass
// is strict: schemaVersion is the compatibility mechanism, so a field the
// version does not define is a 422 naming it rather than ten thousand
// silently mis-read entries.
func parseTicketBoxDump(raw json.RawMessage) (TicketBoxDump, error) {
	var probe struct {
		SchemaVersion *int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return TicketBoxDump{}, fmt.Errorf("%w: the body must be one JSON object", httpx.ErrValidation)
	}
	if probe.SchemaVersion == nil {
		return TicketBoxDump{}, fmt.Errorf("%w: schemaVersion is required", httpx.ErrValidation)
	}
	if *probe.SchemaVersion != ticketBoxSchemaVersion {
		return TicketBoxDump{}, fmt.Errorf(
			"%w: schemaVersion %d is not supported — export from Ticket Box with schema version %d",
			httpx.ErrValidation, *probe.SchemaVersion, ticketBoxSchemaVersion)
	}

	var dump TicketBoxDump
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&dump); err != nil {
		return TicketBoxDump{}, fmt.Errorf("%w: %v", httpx.ErrValidation, err)
	}
	return dump, nil
}

// The plan: the file regrouped by the catalog item each entry belongs to, with
// every value already normalised, so the run needs no further judgement.

type plannedWatch struct {
	at, watchType string
}

// An episode entry keeps the file's episode TMDB id beside its position: the
// id is what the run resolves first (episodeResolver), the position the
// fallback and what a skip reports.
type plannedEpisodeWatch struct {
	season, episode int
	tmdbID          *int64
	at, watchType   string
}

type plannedRating struct {
	value int
	at    string
}

type plannedEpisodeRating struct {
	season, episode int
	tmdbID          *int64
	rating          plannedRating
}

type movieGroup struct {
	tmdbID      int64
	watches     []plannedWatch
	rating      *plannedRating
	watchlistAt *string
	favoriteAt  *string
}

type showGroup struct {
	tmdbID         int64
	watches        []plannedEpisodeWatch
	showRating     *plannedRating
	episodeRatings []plannedEpisodeRating
	watchlistAt    *string
	favoriteAt     *string
}

type restorePlan struct {
	movies []*movieGroup
	shows  []*showGroup
}

// skips expands a whole-group refusal into one skip per entry, so the counts
// stay exact when a show could not be materialised.
func (g *movieGroup) skips(reason string) []RestoreSkip {
	id := g.tmdbID
	var out []RestoreSkip
	for range g.watches {
		out = append(out, RestoreSkip{Kind: restoreKindHistory, Type: "movie", TMDBID: &id, Reason: reason})
	}
	if g.rating != nil {
		out = append(out, RestoreSkip{Kind: restoreKindRating, Type: "movie", TMDBID: &id, Reason: reason})
	}
	if g.watchlistAt != nil {
		out = append(out, RestoreSkip{Kind: restoreKindWatchlist, Type: "movie", TMDBID: &id, Reason: reason})
	}
	if g.favoriteAt != nil {
		out = append(out, RestoreSkip{Kind: restoreKindFavorite, Type: "movie", TMDBID: &id, Reason: reason})
	}
	return out
}

func (g *showGroup) skips(reason string) []RestoreSkip {
	id := g.tmdbID
	var out []RestoreSkip
	for _, w := range g.watches {
		s, e := w.season, w.episode
		out = append(out, RestoreSkip{Kind: restoreKindHistory, Type: "episode", ShowTMDBID: &id, Season: &s, Episode: &e, Reason: reason})
	}
	if g.showRating != nil {
		out = append(out, RestoreSkip{Kind: restoreKindRating, Type: "show", TMDBID: &id, Reason: reason})
	}
	for _, r := range g.episodeRatings {
		s, e := r.season, r.episode
		out = append(out, RestoreSkip{Kind: restoreKindRating, Type: "episode", ShowTMDBID: &id, Season: &s, Episode: &e, Reason: reason})
	}
	if g.watchlistAt != nil {
		out = append(out, RestoreSkip{Kind: restoreKindWatchlist, Type: "show", TMDBID: &id, Reason: reason})
	}
	if g.favoriteAt != nil {
		out = append(out, RestoreSkip{Kind: restoreKindFavorite, Type: "show", TMDBID: &id, Reason: reason})
	}
	return out
}

// planBuilder accumulates groups by TMDB id while the file is walked; the
// maps become sorted slices at the end so the run order is deterministic.
type planBuilder struct {
	movies map[int64]*movieGroup
	shows  map[int64]*showGroup
	skips  []RestoreSkip
	now    time.Time
}

func (b *planBuilder) movie(tmdbID int64) *movieGroup {
	g, ok := b.movies[tmdbID]
	if !ok {
		g = &movieGroup{tmdbID: tmdbID}
		b.movies[tmdbID] = g
	}
	return g
}

func (b *planBuilder) show(tmdbID int64) *showGroup {
	g, ok := b.shows[tmdbID]
	if !ok {
		g = &showGroup{tmdbID: tmdbID}
		b.shows[tmdbID] = g
	}
	return g
}

func (b *planBuilder) skip(kind string, a TicketBoxAddress, reason string) {
	b.skips = append(b.skips, RestoreSkip{
		Kind: kind, Type: a.Type, TMDBID: a.TMDBID, ShowTMDBID: a.ShowTMDBID,
		Season: a.Season, Episode: a.Episode, Reason: reason,
	})
}

// instant normalises one of the file's timestamps, or names the skip reason.
func (b *planBuilder) instant(raw string) (string, string) {
	at, err := common.NormaliseInstant(raw, b.now)
	switch {
	case err == nil:
		return at, ""
	case errors.Is(err, common.ErrInstantFuture):
		return "", skipFutureTimestamp
	default:
		return "", skipInvalidTimestamp
	}
}

// ItemID is a movie's or show's own TMDB id, positive or absent.
func itemID(a TicketBoxAddress) (int64, bool) {
	if a.TMDBID == nil || *a.TMDBID < 1 {
		return 0, false
	}
	return *a.TMDBID, true
}

// episodeAddress is the show id plus position an episode entry must carry.
// Both numbers may be 0 — season 0 is Specials, and a pilot may be episode 0
// — so only negatives are refused, matching resolveTrackedEpisode.
func episodeAddress(a TicketBoxAddress) (showID int64, season, episode int, reason string) {
	if a.ShowTMDBID == nil || *a.ShowTMDBID < 1 {
		return 0, 0, 0, skipMissingTMDBID
	}
	if a.Season == nil || a.Episode == nil || *a.Season < 0 || *a.Episode < 0 {
		return 0, 0, 0, skipMissingPosition
	}
	return *a.ShowTMDBID, *a.Season, *a.Episode, ""
}

// ValidateTicketBoxDump turns the file into a plan and the list of entries
// that can never be taken. Pure and fast — it runs before the job starts, so
// the caller's response already carries the group total and these skips. Per
// entry the first failing check names the reason.
func ValidateTicketBoxDump(d TicketBoxDump, now time.Time) (restorePlan, []RestoreSkip) {
	b := &planBuilder{movies: map[int64]*movieGroup{}, shows: map[int64]*showGroup{}, now: now}

	for _, e := range d.History {
		switch e.Type {
		case "movie":
			id, ok := itemID(e.TicketBoxAddress)
			if !ok {
				b.skip(restoreKindHistory, e.TicketBoxAddress, skipMissingTMDBID)
				continue
			}
			at, reason := b.instant(e.AddedAt)
			if reason != "" {
				b.skip(restoreKindHistory, e.TicketBoxAddress, reason)
				continue
			}
			if !slices.Contains(watchTypes, e.HistoryType) {
				b.skip(restoreKindHistory, e.TicketBoxAddress, skipInvalidWatchType)
				continue
			}
			g := b.movie(id)
			g.watches = append(g.watches, plannedWatch{at: at, watchType: e.HistoryType})
		case "episode":
			showID, season, episode, reason := episodeAddress(e.TicketBoxAddress)
			if reason != "" {
				b.skip(restoreKindHistory, e.TicketBoxAddress, reason)
				continue
			}
			at, reason := b.instant(e.AddedAt)
			if reason != "" {
				b.skip(restoreKindHistory, e.TicketBoxAddress, reason)
				continue
			}
			if !slices.Contains(watchTypes, e.HistoryType) {
				b.skip(restoreKindHistory, e.TicketBoxAddress, skipInvalidWatchType)
				continue
			}
			g := b.show(showID)
			g.watches = append(g.watches, plannedEpisodeWatch{season: season, episode: episode, tmdbID: e.TMDBID, at: at, watchType: e.HistoryType})
		default:
			b.skip(restoreKindHistory, e.TicketBoxAddress, skipUnknownType)
		}
	}

	// Ratings: a later entry for the same target wins, which is what the
	// upsert would do anyway — deciding it here keeps the count honest.
	episodeRatings := map[int64]map[episodeKey]plannedEpisodeRating{}
	for _, e := range d.Ratings {
		switch e.Type {
		case "season":
			b.skip(restoreKindRating, e.TicketBoxAddress, skipSeasonRating)
			continue
		case "movie", "show", "episode":
		default:
			b.skip(restoreKindRating, e.TicketBoxAddress, skipUnknownType)
			continue
		}
		if e.Rating == nil || common.AssertRatingInRange(*e.Rating) != nil {
			b.skip(restoreKindRating, e.TicketBoxAddress, skipRatingOutOfRange)
			continue
		}
		switch e.Type {
		case "movie", "show":
			id, ok := itemID(e.TicketBoxAddress)
			if !ok {
				b.skip(restoreKindRating, e.TicketBoxAddress, skipMissingTMDBID)
				continue
			}
			at, reason := b.instant(e.RatedAt)
			if reason != "" {
				b.skip(restoreKindRating, e.TicketBoxAddress, reason)
				continue
			}
			r := plannedRating{value: *e.Rating, at: at}
			if e.Type == "movie" {
				b.movie(id).rating = &r
			} else {
				b.show(id).showRating = &r
			}
		case "episode":
			showID, season, episode, reason := episodeAddress(e.TicketBoxAddress)
			if reason != "" {
				b.skip(restoreKindRating, e.TicketBoxAddress, reason)
				continue
			}
			at, reason := b.instant(e.RatedAt)
			if reason != "" {
				b.skip(restoreKindRating, e.TicketBoxAddress, reason)
				continue
			}
			if episodeRatings[showID] == nil {
				episodeRatings[showID] = map[episodeKey]plannedEpisodeRating{}
			}
			episodeRatings[showID][episodeKey{season, episode}] = plannedEpisodeRating{
				season: season, episode: episode, tmdbID: e.TMDBID,
				rating: plannedRating{value: *e.Rating, at: at},
			}
		}
	}
	for showID, byKey := range episodeRatings {
		g := b.show(showID)
		for _, r := range byKey {
			g.episodeRatings = append(g.episodeRatings, r)
		}
		slices.SortFunc(g.episodeRatings, func(x, y plannedEpisodeRating) int {
			if x.season != y.season {
				return x.season - y.season
			}
			return x.episode - y.episode
		})
	}

	for _, e := range d.Watchlist {
		switch e.Type {
		case "season", "episode":
			b.skip(restoreKindWatchlist, e.TicketBoxAddress, skipWatchlistTarget)
			continue
		case "movie", "show":
		default:
			b.skip(restoreKindWatchlist, e.TicketBoxAddress, skipUnknownType)
			continue
		}
		id, ok := itemID(e.TicketBoxAddress)
		if !ok {
			b.skip(restoreKindWatchlist, e.TicketBoxAddress, skipMissingTMDBID)
			continue
		}
		at, reason := b.instant(e.ListedAt)
		if reason != "" {
			b.skip(restoreKindWatchlist, e.TicketBoxAddress, reason)
			continue
		}
		if e.Type == "movie" {
			b.movie(id).watchlistAt = &at
		} else {
			b.show(id).watchlistAt = &at
		}
	}

	// Favorites are movies and shows only, as in sofalog (D-030); Ticket Box
	// writes no other kind, so anything else is simply an unknown type.
	for _, e := range d.Favorites {
		if e.Type != "movie" && e.Type != "show" {
			b.skip(restoreKindFavorite, e.TicketBoxAddress, skipUnknownType)
			continue
		}
		id, ok := itemID(e.TicketBoxAddress)
		if !ok {
			b.skip(restoreKindFavorite, e.TicketBoxAddress, skipMissingTMDBID)
			continue
		}
		at, reason := b.instant(e.FavoritedAt)
		if reason != "" {
			b.skip(restoreKindFavorite, e.TicketBoxAddress, reason)
			continue
		}
		if e.Type == "movie" {
			b.movie(id).favoriteAt = &at
		} else {
			b.show(id).favoriteAt = &at
		}
	}

	plan := restorePlan{
		movies: make([]*movieGroup, 0, len(b.movies)),
		shows:  make([]*showGroup, 0, len(b.shows)),
	}
	for _, g := range b.movies {
		plan.movies = append(plan.movies, g)
	}
	for _, g := range b.shows {
		plan.shows = append(plan.shows, g)
	}
	slices.SortFunc(plan.movies, func(x, y *movieGroup) int { return int(x.tmdbID - y.tmdbID) })
	slices.SortFunc(plan.shows, func(x, y *showGroup) int { return int(x.tmdbID - y.tmdbID) })
	return plan, b.skips
}

// The job runner: one restore per user, in memory, for as long as the process
// lives. A table would survive a restart, but resuming is the hard part of one
// — which groups committed, whether a half-materialised show is whole — and
// replace semantics already make "run it again" the correct recovery. So a
// restart loses the progress display and nothing else: every committed group
// stays committed, and the next run converges on the same end state.

type restoreRunner struct {
	mu   sync.Mutex
	jobs map[int64]*restoreJob
	wg   sync.WaitGroup
}

func newRestoreRunner() *restoreRunner {
	return &restoreRunner{jobs: map[int64]*restoreJob{}}
}

// restoreJob is the mutable record behind one RestoreJob snapshot. Every field
// write goes through the mutex; readers get a deep copy.
type restoreJob struct {
	mu     sync.Mutex
	snap   RestoreJob
	cancel context.CancelFunc
}

func NewRestoreJob(startedAt time.Time, plan restorePlan, skips []RestoreSkip) *restoreJob {
	j := &restoreJob{snap: RestoreJob{
		Kind:      restoreKindTicketBox,
		State:     restoreStateRunning,
		StartedAt: startedAt.Format(time.RFC3339),
		Progress:  RestoreProgress{GroupsTotal: len(plan.movies) + len(plan.shows)},
		Report: RestoreReport{
			MoviesTotal:     len(plan.movies),
			ShowsTotal:      len(plan.shows),
			SkippedByReason: map[string]int{},
			Skipped:         []RestoreSkip{},
		},
	}}
	j.skipAll(skips)
	return j
}

// update applies fn under the lock. Every mutation goes through here so the
// lock discipline lives in one place.
func (j *restoreJob) update(fn func(*RestoreJob)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.snap)
}

func (j *restoreJob) setCurrent(cur *RestoreCurrent) {
	j.update(func(s *RestoreJob) { s.Progress.Current = cur })
}

func (j *restoreJob) groupDone() {
	j.update(func(s *RestoreJob) {
		s.Progress.GroupsDone++
		s.Progress.Current = nil
	})
}

func (j *restoreJob) skipAll(skips []RestoreSkip) {
	if len(skips) == 0 {
		return
	}
	j.update(func(s *RestoreJob) {
		for _, sk := range skips {
			s.Report.SkippedTotal++
			s.Report.SkippedByReason[sk.Reason]++
			if len(s.Report.Skipped) < restoreSkipCap {
				s.Report.Skipped = append(s.Report.Skipped, sk)
			}
		}
	})
}

func (j *restoreJob) finish(state string, at time.Time, jobErr *RestoreError) {
	j.update(func(s *RestoreJob) {
		s.State = state
		finished := at.Format(time.RFC3339)
		s.FinishedAt = &finished
		s.Progress.Current = nil
		s.Error = jobErr
	})
}

func (j *restoreJob) running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap.State == restoreStateRunning
}

// snapshot deep-copies the record: the slice, the map and every pointer, so a
// caller encoding the result races with nothing.
func (j *restoreJob) Snapshot() RestoreJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.snap
	out.Report.Skipped = slices.Clone(j.snap.Report.Skipped)
	out.Report.SkippedByReason = make(map[string]int, len(j.snap.Report.SkippedByReason))
	for k, v := range j.snap.Report.SkippedByReason {
		out.Report.SkippedByReason[k] = v
	}
	if j.snap.FinishedAt != nil {
		v := *j.snap.FinishedAt
		out.FinishedAt = &v
	}
	if j.snap.Progress.Current != nil {
		cur := *j.snap.Progress.Current
		if cur.Title != nil {
			t := *cur.Title
			cur.Title = &t
		}
		out.Progress.Current = &cur
	}
	if j.snap.Error != nil {
		e := *j.snap.Error
		out.Error = &e
	}
	return out
}

// start registers a job for the user, refusing while one is still running.
// A finished job is replaced: the report is the SPA's to have read by then.
func (r *restoreRunner) start(userID int64, job *restoreJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.jobs[userID]; ok && existing.running() {
		return fmt.Errorf("%w: a restore is already running for this account", httpx.ErrConflict)
	}
	r.jobs[userID] = job
	return nil
}

func (r *restoreRunner) get(userID int64) (*restoreJob, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[userID]
	return job, ok
}

func (r *restoreRunner) cancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range r.jobs {
		if job.cancel != nil {
			job.cancel()
		}
	}
}

// StartTicketBoxRestore validates the file synchronously — a version or shape
// problem is this request's 422, and the returned snapshot already carries the
// group total and the entries validation refused — then runs the import as a
// job and returns its first snapshot. One job per user at a time; a second
// start while one runs is a 409.
func (s *Service) StartTicketBoxRestore(ctx context.Context, userID int64, raw json.RawMessage) (RestoreJob, error) {
	dump, err := parseTicketBoxDump(raw)
	if err != nil {
		return RestoreJob{}, err
	}
	now := s.Now().UTC()
	plan, skips := ValidateTicketBoxDump(dump, now)

	// The job outlives the request, so it must not inherit the request's
	// cancellation — that fires the moment the 202 is written. WithoutCancel
	// keeps the request's values (the request id in log lines) and drops the
	// deadline; the runner's own cancel is what shutdown pulls.
	jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	job := NewRestoreJob(now, plan, skips)
	job.cancel = cancel
	if err := s.restore.start(userID, job); err != nil {
		cancel()
		return RestoreJob{}, err
	}

	s.restore.wg.Add(1)
	go func() {
		defer s.restore.wg.Done()
		defer cancel()
		defer func() {
			// httpx.Recover guards handlers, not goroutines: a panic here
			// would take the process down with it.
			if p := recover(); p != nil {
				slog.Error("restore: panic", "user_id", userID, "panic", p)
				job.finish(restoreStateFailed, s.Now().UTC(),
					&RestoreError{Code: restoreErrInternal, Message: restoreInternalErrMessage})
			}
		}()
		s.RunTicketBoxRestore(jobCtx, userID, plan, job)
	}()
	return job.Snapshot(), nil
}

// TicketBoxRestoreStatus is the poll target: this user's running job, or the
// last one's report. ErrNotFound when the user has started none since boot —
// which is also what a restart leaves behind.
func (s *Service) TicketBoxRestoreStatus(userID int64) (RestoreJob, error) {
	job, ok := s.restore.get(userID)
	if !ok {
		return RestoreJob{}, fmt.Errorf("%w: no restore has been started", httpx.ErrNotFound)
	}
	return job.Snapshot(), nil
}

// RunTicketBoxRestore does the work, group by group, and never returns an
// error: everything it cannot absorb ends the job as failed with the reason
// on the record. Synchronous so a test can drive it without the runner.
//
// Per group the order is the tracking writes' own — resolve (and materialise)
// outside any transaction, then ONE transaction for the group's rows and its
// single status recompute, then the asset reconcile. The clock is read once
// per group, not once per run: a whole-library import can cross midnight,
// and D-012's aired predicate must not be a day behind by the end. The file's
// timestamps move watched_at and nothing else.
func (s *Service) RunTicketBoxRestore(ctx context.Context, userID int64, plan restorePlan, job *restoreJob) {
	var breaker common.ProviderBreaker
	fail := func(code, message string) {
		s.enrichRestoreSkips(ctx, job)
		job.finish(restoreStateFailed, s.Now().UTC(), &RestoreError{Code: code, Message: message})
	}
	internal := func(stage string, err error) {
		slog.Error("restore: "+stage, "user_id", userID, "err", err)
		fail(restoreErrInternal, restoreInternalErrMessage)
	}

	for _, g := range plan.movies {
		if ctx.Err() != nil {
			fail(restoreErrCancelled, restoreCancelledErrMessage)
			return
		}
		job.setCurrent(&RestoreCurrent{Type: restoreGroupMovie, TMDBID: g.tmdbID})

		movie, materialised, err := s.ResolveRestoreMovie(ctx, g.tmdbID, &breaker)
		if err != nil {
			reason, ok := common.GroupSkipReason(err, common.SkipMovieNotFound, common.SkipMovieUnavailable)
			if !ok {
				internal("resolving movie", err)
				return
			}
			job.skipAll(g.skips(reason))
			job.groupDone()
			continue
		}
		job.setCurrent(&RestoreCurrent{Type: restoreGroupMovie, TMDBID: g.tmdbID, Title: &movie.Title})

		if err := s.restoreMovieGroup(ctx, userID, movie, g, job); err != nil {
			internal("writing movie", err)
			return
		}
		if materialised {
			job.update(func(r *RestoreJob) { r.Report.MoviesMaterialised++ })
		}
		s.SyncMovieAssets(ctx, movie)
		job.groupDone()
	}

	for _, g := range plan.shows {
		if ctx.Err() != nil {
			fail(restoreErrCancelled, restoreCancelledErrMessage)
			return
		}
		job.setCurrent(&RestoreCurrent{Type: restoreGroupShow, TMDBID: g.tmdbID})

		show, materialised, err := s.ResolveRestoreShow(ctx, g.tmdbID, &breaker)
		if err != nil {
			reason, ok := common.GroupSkipReason(err, common.SkipShowNotFound, common.SkipShowUnavailable)
			if !ok {
				internal("resolving show", err)
				return
			}
			job.skipAll(g.skips(reason))
			job.groupDone()
			continue
		}
		job.setCurrent(&RestoreCurrent{Type: restoreGroupShow, TMDBID: g.tmdbID, Title: &show.Title})

		if err := s.restoreShowGroup(ctx, userID, show, g, job); err != nil {
			internal("writing show", err)
			return
		}
		if materialised {
			job.update(func(r *RestoreJob) { r.Report.ShowsMaterialised++ })
		}
		s.SyncShowAssets(ctx, show)
		job.groupDone()
	}

	if ctx.Err() != nil {
		fail(restoreErrCancelled, restoreCancelledErrMessage)
		return
	}
	s.enrichRestoreSkips(ctx, job)
	job.finish(restoreStateDone, s.Now().UTC(), nil)
}

// enrichRestoreSkips names the item behind every recorded skip that does not
// carry a title yet — the validation-time ones, and whole-group skips — from
// the local catalog, once, as the job ends: by then every item the run
// materialised is there to be named. Point reads, memoised per id, bounded by
// restoreSkipCap. Never a provider call and never a failure: a miss leaves the
// title null, and any other error is logged and leaves it null too. The reads
// run on a context detached from cancellation, so a job cancelled at shutdown
// still names what it can.
func (s *Service) enrichRestoreSkips(ctx context.Context, job *restoreJob) {
	ctx = context.WithoutCancel(ctx)
	skipped := job.Snapshot().Report.Skipped

	type itemKey struct {
		movie bool
		id    int64
	}
	titles := map[itemKey]*string{}
	lookup := func(k itemKey) *string {
		if t, ok := titles[k]; ok {
			return t
		}
		var title *string
		var err error
		if k.movie {
			var row common.MovieRow
			if row, err = common.GetMovieByTMDBID(ctx, s.DB, k.id); err == nil {
				title = &row.Title
			}
		} else {
			var row common.ShowRow
			if row, err = common.GetShowByTMDBID(ctx, s.DB, k.id); err == nil {
				title = &row.Title
			}
		}
		if err != nil && !errors.Is(err, httpx.ErrNotFound) {
			slog.Warn("restore: naming a skipped item", "tmdb_id", k.id, "err", err)
		}
		titles[k] = title
		return title
	}

	changed := false
	for i, sk := range skipped {
		if sk.Title != nil {
			continue
		}
		var k itemKey
		switch {
		case sk.Type == "movie" && sk.TMDBID != nil:
			k = itemKey{movie: true, id: *sk.TMDBID}
		case sk.ShowTMDBID != nil:
			k = itemKey{id: *sk.ShowTMDBID}
		case sk.Type == "show" && sk.TMDBID != nil:
			k = itemKey{id: *sk.TMDBID}
		default:
			continue
		}
		if t := lookup(k); t != nil {
			skipped[i].Title = t
			changed = true
		}
	}
	if changed {
		job.update(func(r *RestoreJob) { r.Report.Skipped = skipped })
	}
}

// restoreMovieGroup writes one movie's rows in one transaction. The replace
// happens only when the file has watches for the movie; a rating's updated_at
// is the file's ratedAt — a restore reproduces the source's record, where
// MarkWatched stamps a fresh opinion with the real clock. The recompute runs
// exactly when the log changed: a rating is not a watch (D-010).
func (s *Service) restoreMovieGroup(ctx context.Context, userID int64, movie common.MovieRow, g *movieGroup, job *restoreJob) error {
	at := s.Now().UTC()
	now := at.Format(time.RFC3339)

	var inserted, ratings, watchlist, favorites int
	var replaced int64
	err := s.DB.InTx(ctx, func(tx db.Tx) error {
		if len(g.watches) > 0 {
			n, err := deleteMovieWatches(ctx, tx, userID, movie.ID)
			if err != nil {
				return err
			}
			replaced = n
			for _, w := range g.watches {
				if err := common.InsertWatch(ctx, tx, userID, "movie", movie.ID, w.at, w.watchType); err != nil {
					return err
				}
				inserted++
			}
		}
		if g.rating != nil {
			if err := common.UpsertVideoRating(ctx, tx, userID, "movie", movie.ID, g.rating.value, g.rating.at); err != nil {
				return err
			}
			if err := common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindRating, "movie", movie.ID, "set", &g.rating.value, now); err != nil {
				return err
			}
			ratings++
		}
		if g.watchlistAt != nil {
			created, err := common.InsertWatchlist(ctx, tx, userID, "movie", movie.ID, *g.watchlistAt)
			if err != nil {
				return err
			}
			if created {
				if err := common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindWatchlist, "movie", movie.ID, "set", nil, now); err != nil {
					return err
				}
				watchlist++
			}
		}
		if g.favoriteAt != nil {
			created, err := common.InsertFavorite(ctx, tx, userID, "movie", movie.ID, *g.favoriteAt)
			if err != nil {
				return err
			}
			if created {
				if err := common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindFavorite, "movie", movie.ID, "set", nil, now); err != nil {
					return err
				}
				favorites++
			}
		}
		if len(g.watches) > 0 {
			return common.RecomputeMovieStatus(ctx, tx, userID, movie.ID, now)
		}
		return nil
	})
	if err != nil {
		return err
	}
	job.update(func(r *RestoreJob) {
		r.Report.HistoryInserted += inserted
		r.Report.HistoryReplaced += int(replaced)
		r.Report.RatingsUpserted += ratings
		r.Report.WatchlistInserted += watchlist
		r.Report.FavoritesInserted += favorites
	})
	if ratings > 0 || watchlist > 0 || favorites > 0 {
		s.KickTMDBSync()
	}
	return nil
}

// episodeResolver maps a backup's episode entry to a local episode id: by the
// entry's own TMDB episode id first, through the show's stored season
// documents, and by position only when the file has no id or the show's
// listing does not know it. The id is what survives TMDB renumbering a
// season — Top Gear's specials moved from 0x1… to 0x401… — while the export's
// positions are whatever Ticket Box cached. Restore-only: every other path
// keeps addressing episodes by position.
type episodeResolver struct {
	ids     map[episodeKey]int64 // position → local id
	byTMDB  map[int64]episodeKey // TMDB episode id → position, this show only
	seasons map[int][]int        // season → its episode numbers, ascending
}

// restoreSeasonSnapshot is the slice of a stored season document the resolver
// reads. A local shape, not provider.TMDBTVSeasonEpisode: nothing else needs
// an episode's TMDB id, so the shared type does not grow one.
type restoreSeasonSnapshot struct {
	Episodes []struct {
		ID            int64 `json:"id"`
		EpisodeNumber int   `json:"episode_number"`
	} `json:"episodes"`
}

func (s *Service) newEpisodeResolver(ctx context.Context, showID int64) (episodeResolver, error) {
	ids, err := episodeIDsByShow(ctx, s.DB, showID)
	if err != nil {
		return episodeResolver{}, err
	}
	docs, err := seasonDocumentsByShow(ctx, s.DB, showID)
	if err != nil {
		return episodeResolver{}, err
	}
	byTMDB := make(map[int64]episodeKey)
	for _, doc := range docs {
		var snap restoreSeasonSnapshot
		// An undecodable snapshot just resolves nothing by id; the position
		// fallback still applies, as it does for every season on the page.
		if json.Unmarshal([]byte(doc.MetadataJSON), &snap) != nil {
			continue
		}
		for _, e := range snap.Episodes {
			if e.ID > 0 {
				byTMDB[e.ID] = episodeKey{doc.SeasonNumber, e.EpisodeNumber}
			}
		}
	}
	seasons := make(map[int][]int)
	for k := range ids {
		seasons[k.Season] = append(seasons[k.Season], k.Episode)
	}
	for _, numbers := range seasons {
		slices.Sort(numbers)
	}
	return episodeResolver{ids: ids, byTMDB: byTMDB, seasons: seasons}, nil
}

// episodeSkipHint deduces why a position names no episode, from the numbers
// TMDB lists for that season (ascending; nil when it lists none). The causes
// seen in real exports: TMDB numbers specials its own way; and it lists a
// two-part episode as ONE double-length episode, either leaving the second
// number out mid-season or ending the season one short of Trakt. last is the
// season's highest number, for the two hints that cite it.
func episodeSkipHint(numbers []int, season, episode int) (hint string, last *int) {
	if season == 0 {
		return hintSpecialsNumbering, nil
	}
	if len(numbers) == 0 {
		return hintSeasonMissing, nil
	}
	highest := numbers[len(numbers)-1]
	if episode > highest {
		return hintPastSeasonEnd, &highest
	}
	return hintNumberingGap, &highest
}

// resolve returns the local episode id for one entry, or false when neither
// the id nor the position names an episode the show has.
func (r episodeResolver) resolve(tmdbID *int64, season, episode int) (int64, bool) {
	if tmdbID != nil {
		if k, ok := r.byTMDB[*tmdbID]; ok {
			if id, ok := r.ids[k]; ok {
				return id, true
			}
		}
	}
	id, ok := r.ids[episodeKey{season, episode}]
	return id, ok
}

// restoreShowGroup is restoreMovieGroup for a show. Episodes are resolved by
// episodeResolver against the show's local episode rows; an entry it cannot
// place is a skipped entry, and the rest of the group proceeds. The replace
// covers the whole show — every season, Specials included — because that is
// the set the file's episode watches stand in for.
func (s *Service) restoreShowGroup(ctx context.Context, userID int64, show common.ShowRow, g *showGroup, job *restoreJob) error {
	resolver, err := s.newEpisodeResolver(ctx, show.ID)
	if err != nil {
		return err
	}

	type resolvedWatch struct {
		episodeID     int64
		at, watchType string
	}
	type resolvedRating struct {
		episodeID int64
		rating    plannedRating
	}
	var skips []RestoreSkip
	showID := g.tmdbID
	notFound := func(kind string, season, episode int) {
		hint, last := episodeSkipHint(resolver.seasons[season], season, episode)
		skips = append(skips, RestoreSkip{Kind: kind, Type: "episode",
			ShowTMDBID: &showID, Season: &season, Episode: &episode, Reason: common.SkipEpisodeNotFound,
			Title: &show.Title, Hint: &hint, TMDBLastEpisode: last})
	}
	watches := make([]resolvedWatch, 0, len(g.watches))
	for _, w := range g.watches {
		id, ok := resolver.resolve(w.tmdbID, w.season, w.episode)
		if !ok {
			notFound(restoreKindHistory, w.season, w.episode)
			continue
		}
		watches = append(watches, resolvedWatch{episodeID: id, at: w.at, watchType: w.watchType})
	}
	episodeRatings := make([]resolvedRating, 0, len(g.episodeRatings))
	for _, r := range g.episodeRatings {
		id, ok := resolver.resolve(r.tmdbID, r.season, r.episode)
		if !ok {
			notFound(restoreKindRating, r.season, r.episode)
			continue
		}
		episodeRatings = append(episodeRatings, resolvedRating{episodeID: id, rating: r.rating})
	}

	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return err
	}
	now, today := clk.Now, clk.Today

	var inserted, ratings, watchlist, favorites int
	var replaced int64
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		if len(g.watches) > 0 {
			n, err := deleteShowEpisodeWatches(ctx, tx, userID, show.ID)
			if err != nil {
				return err
			}
			replaced = n
			for _, w := range watches {
				if err := common.InsertWatch(ctx, tx, userID, "episode", w.episodeID, w.at, w.watchType); err != nil {
					return err
				}
				inserted++
			}
		}
		if g.showRating != nil {
			if err := common.UpsertVideoRating(ctx, tx, userID, "show", show.ID, g.showRating.value, g.showRating.at); err != nil {
				return err
			}
			if err := common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindRating, "show", show.ID, "set", &g.showRating.value, now); err != nil {
				return err
			}
			ratings++
		}
		for _, r := range episodeRatings {
			if err := common.UpsertVideoRating(ctx, tx, userID, "episode", r.episodeID, r.rating.value, r.rating.at); err != nil {
				return err
			}
			if err := common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindRating, "episode", r.episodeID, "set", &r.rating.value, now); err != nil {
				return err
			}
			ratings++
		}
		if g.watchlistAt != nil {
			created, err := common.InsertWatchlist(ctx, tx, userID, "show", show.ID, *g.watchlistAt)
			if err != nil {
				return err
			}
			if created {
				if err := common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindWatchlist, "show", show.ID, "set", nil, now); err != nil {
					return err
				}
				watchlist++
			}
		}
		if g.favoriteAt != nil {
			created, err := common.InsertFavorite(ctx, tx, userID, "show", show.ID, *g.favoriteAt)
			if err != nil {
				return err
			}
			if created {
				if err := common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindFavorite, "show", show.ID, "set", nil, now); err != nil {
					return err
				}
				favorites++
			}
		}
		// The gate is "the file had watches", not "rows were inserted": a
		// group whose every episode was unknown still emptied the old log.
		if len(g.watches) > 0 {
			return common.RecomputeShowStatus(ctx, tx, userID, show.ID, today, now)
		}
		return nil
	})
	if err != nil {
		return err
	}
	job.skipAll(skips)
	job.update(func(r *RestoreJob) {
		r.Report.HistoryInserted += inserted
		r.Report.HistoryReplaced += int(replaced)
		r.Report.RatingsUpserted += ratings
		r.Report.WatchlistInserted += watchlist
		r.Report.FavoritesInserted += favorites
	})
	if ratings > 0 || watchlist > 0 || favorites > 0 {
		s.KickTMDBSync()
	}
	return nil
}
