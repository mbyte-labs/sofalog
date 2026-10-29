package jellyfin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// The Jellyfin library scan (D-033): SPEC §5's seventh call site. A background
// read of the household's own Jellyfin server that answers one question —
// which catalog movies and seasons are on it — and stores the answer in the
// shared jellyfin_library table, which the details views read into their
// copies lists as the `jellyfin` kind.
//
// Three rules hold it:
//
//   - It is a pure Jellyfin read plus a local join. It never calls TMDB, never
//     materialises an item (an untracked movie on the server is simply not
//     matched until somebody tracks it), and writes no per-user row.
//   - While a server is configured the scan OWNS the jellyfin kind: it adds
//     and removes, the copy routes refuse the kind with a 422, and the tile is
//     read-only in the SPA. Unconfigured, nothing here runs and the kind stays
//     the hand-set label D-021 described.
//   - Presence, never playback. Nothing it reads touches the watch history —
//     §9's exclusion of scrobbling is unchanged.

const (
	// jellyfinBootDelay is how long after boot the first scheduled scan runs:
	// enough for the server, often in the same compose stack, to be up.
	jellyfinBootDelay = 2 * time.Minute
)

// jellyfinRunner is the scan's state: its WaitGroup, the wake-up channel the
// settings page's "scan now" kicks, the schedule, and the last report — in
// memory, one for the whole server, forgotten on restart like the restore job.
type jellyfinRunner struct {
	wg        sync.WaitGroup
	kick      chan struct{}
	interval  time.Duration
	bootDelay time.Duration

	mu         sync.Mutex
	running    bool
	serverName string
	last       *JellyfinScanReport
}

func newJellyfinRunner(interval time.Duration) *jellyfinRunner {
	return &jellyfinRunner{kick: make(chan struct{}, 1), interval: interval, bootDelay: jellyfinBootDelay}
}

// JellyfinScanReport is one scan's outcome, the settings page's report.
type JellyfinScanReport struct {
	StartedAt  string `json:"started_at"`  // RFC3339 UTC
	FinishedAt string `json:"finished_at"` // RFC3339 UTC
	// What the server holds — movies and series with a TMDB id, and episode
	// files with a season number — before matching.
	JellyfinMovies   int `json:"jellyfin_movies"`
	JellyfinSeries   int `json:"jellyfin_series"`
	JellyfinEpisodes int `json:"jellyfin_episodes"`
	// What matched a catalog row and was written.
	MatchedMovies  int `json:"matched_movies"`
	MatchedSeasons int `json:"matched_seasons"`
	// Error is null on success; otherwise a stable code — `auth_failed` (the
	// key was refused), `provider_unavailable` (the server could not be
	// reached or answered badly), `internal`. On an error nothing was written.
	Error *string `json:"error"`
}

// JellyfinStatus is the settings page's one read: whether a server is
// configured, what it calls itself, whether a scan is running, the schedule
// and the last report.
type JellyfinStatus struct {
	Configured bool `json:"configured"`
	// ServerName is what the server reported on the last successful ping;
	// null until one succeeds (or when not configured).
	ServerName *string `json:"server_name"`
	Scanning   bool    `json:"scanning"`
	// IntervalSeconds is the scheduled cadence; 0 means only "scan now" runs.
	IntervalSeconds int64               `json:"interval_seconds"`
	Last            *JellyfinScanReport `json:"last"`
}

const (
	jellyfinErrAuth        = "auth_failed"
	jellyfinErrUnavailable = "provider_unavailable"
	jellyfinErrInternal    = "internal"
)

// JellyfinStatus is the settings page's read. A pure local read.
func (s *Service) JellyfinStatus() JellyfinStatus {
	st := JellyfinStatus{Configured: s.JellyfinSynced()}
	if !st.Configured || s.jellyfinScan == nil {
		return st
	}
	r := s.jellyfinScan
	r.mu.Lock()
	defer r.mu.Unlock()
	st.Scanning = r.running
	st.IntervalSeconds = int64(r.interval / time.Second)
	if r.serverName != "" {
		name := r.serverName
		st.ServerName = &name
	}
	if r.last != nil {
		last := *r.last
		st.Last = &last
	}
	return st
}

// StartJellyfinScan kicks the scan loop: "scan now" from the settings page.
// ErrNotFound when no server is configured (there is nothing to scan), 409
// while one is already running — a second one would only repeat it. Any
// signed-in user may press it: the scan is server-wide, idempotent and cheap.
func (s *Service) StartJellyfinScan() error {
	if !s.JellyfinSynced() || s.jellyfinScan == nil {
		return fmt.Errorf("%w: no Jellyfin server is configured", httpx.ErrNotFound)
	}
	r := s.jellyfinScan
	r.mu.Lock()
	running := r.running
	r.mu.Unlock()
	if running {
		return fmt.Errorf("%w: a Jellyfin scan is already running", httpx.ErrConflict)
	}
	select {
	case r.kick <- struct{}{}:
	default:
	}
	return nil
}

// jellyfinLoop runs the scan on its schedule and on every kick until ctx
// ends. Started by Run. A zero interval never arms the timer, so only kicks
// run it; the boot delay still applies to the first scheduled one.
func (s *Service) jellyfinLoop(ctx context.Context) {
	r := s.jellyfinScan
	timer := time.NewTimer(r.bootDelay)
	if r.interval == 0 {
		timer.Stop()
	}
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-r.kick:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		rep := s.runJellyfinScan(ctx)
		if rep.Error != nil {
			slog.Warn("jellyfin scan: failed", "error", *rep.Error)
		} else {
			slog.Info("jellyfin scan: done",
				"jellyfin_movies", rep.JellyfinMovies, "jellyfin_series", rep.JellyfinSeries,
				"jellyfin_episodes", rep.JellyfinEpisodes,
				"matched_movies", rep.MatchedMovies, "matched_seasons", rep.MatchedSeasons)
		}
		if r.interval > 0 {
			timer.Reset(r.interval)
		}
	}
}

// runJellyfinScan performs one scan: read the server, match against the
// catalog, replace the table. Synchronous and report-returning so a test
// drives it without the loop; it never returns an error and recovers its own
// panics, because httpx.Recover guards handlers, not this goroutine. On any
// failure the table is left exactly as the previous scan wrote it.
func (s *Service) runJellyfinScan(ctx context.Context) (rep JellyfinScanReport) {
	r := s.jellyfinScan
	rep.StartedAt = s.Now().UTC().Format(time.RFC3339)
	if r != nil {
		r.mu.Lock()
		r.running = true
		r.mu.Unlock()
	}
	defer func() {
		if p := recover(); p != nil {
			slog.Error("jellyfin scan: panic", "panic", p)
			rep.Error = ptrString(jellyfinErrInternal)
		}
		rep.FinishedAt = s.Now().UTC().Format(time.RFC3339)
		if r != nil {
			r.mu.Lock()
			r.running = false
			last := rep
			r.last = &last
			r.mu.Unlock()
		}
	}()

	fail := func(err error) JellyfinScanReport {
		switch {
		case errors.Is(err, provider.ErrJellyfinAuth):
			rep.Error = ptrString(jellyfinErrAuth)
		case errors.Is(err, httpx.ErrProviderUnavailable):
			rep.Error = ptrString(jellyfinErrUnavailable)
		default:
			rep.Error = ptrString(jellyfinErrInternal)
		}
		slog.Warn("jellyfin scan: aborted", "err", err)
		return rep
	}

	info, err := s.Jellyfin.Ping(ctx)
	if err != nil {
		return fail(err)
	}
	if r != nil && info.Name != "" {
		r.mu.Lock()
		r.serverName = info.Name
		r.mu.Unlock()
	}
	movies, err := s.Jellyfin.Movies(ctx)
	if err != nil {
		return fail(err)
	}
	series, err := s.Jellyfin.Series(ctx)
	if err != nil {
		return fail(err)
	}
	episodes, err := s.Jellyfin.Episodes(ctx)
	if err != nil {
		return fail(err)
	}
	rep.JellyfinMovies, rep.JellyfinSeries, rep.JellyfinEpisodes = len(movies), len(series), len(episodes)

	// The local keys, read once each — never a query per Jellyfin item.
	movieIDs, err := movieIDsByTMDB(ctx, s.DB)
	if err != nil {
		return fail(err)
	}
	seasonIDs, err := seasonIDsByShowTMDB(ctx, s.DB)
	if err != nil {
		return fail(err)
	}

	entries := matchJellyfinLibrary(movies, series, episodes, movieIDs, seasonIDs)
	for _, e := range entries {
		if e.targetType == "movie" {
			rep.MatchedMovies++
		} else {
			rep.MatchedSeasons++
		}
	}
	seenAt := s.Now().UTC().Format(time.RFC3339)
	err = s.DB.InTx(ctx, func(tx db.Tx) error {
		return replaceJellyfinLibrary(ctx, tx, entries, seenAt)
	})
	if err != nil {
		return fail(err)
	}
	return rep
}

// matchJellyfinLibrary is the join, in Go: a movie matches on its TMDB id; a
// season matches when its series' TMDB id is known AND at least one episode
// file sits in that season number (D-033 — "any episode present"), and the
// row counts the DISTINCT episode numbers found there so a page can tell a
// complete season from a partial one (a file with no episode number counts
// once). Specials (season 0) match like any other season: the row exists, and
// the season page manages it. Duplicates on the Jellyfin side (a movie held
// twice) fold into one row; the first Jellyfin id seen is kept.
func matchJellyfinLibrary(movies, series []provider.JellyfinItem, episodes []provider.JellyfinEpisode,
	movieIDs map[int64]int64, seasonIDs map[jellyfinSeasonKey]int64) []jellyfinEntry {
	var entries []jellyfinEntry
	index := make(map[jellyfinEntry]int) // (type, id) -> position in entries
	add := func(e jellyfinEntry) int {
		key := jellyfinEntry{targetType: e.targetType, targetID: e.targetID}
		if i, ok := index[key]; ok {
			return i
		}
		index[key] = len(entries)
		entries = append(entries, e)
		return len(entries) - 1
	}

	for _, m := range movies {
		if id, ok := movieIDs[m.TMDBID]; ok {
			add(jellyfinEntry{targetType: "movie", targetID: id, jellyfinID: m.ID})
		}
	}

	seriesTMDB := make(map[string]int64, len(series))
	for _, sr := range series {
		if _, dup := seriesTMDB[sr.ID]; !dup {
			seriesTMDB[sr.ID] = sr.TMDBID
		}
	}
	type fileKey struct {
		seasonID int64
		episode  int
	}
	seenFiles := make(map[fileKey]bool)
	for _, ep := range episodes {
		showTMDBID, ok := seriesTMDB[ep.SeriesID]
		if !ok {
			continue
		}
		id, ok := seasonIDs[jellyfinSeasonKey{showTMDBID: showTMDBID, number: ep.Season}]
		if !ok {
			continue
		}
		i := add(jellyfinEntry{targetType: "season", targetID: id, jellyfinID: ep.SeriesID})
		if ep.Episode > 0 && seenFiles[fileKey{seasonID: id, episode: ep.Episode}] {
			continue
		}
		seenFiles[fileKey{seasonID: id, episode: ep.Episode}] = true
		entries[i].episodes++
	}
	// Deterministic order for the insert and the tests: movies first, then
	// seasons, each by local id.
	slices.SortFunc(entries, func(a, b jellyfinEntry) int {
		if a.targetType != b.targetType {
			if a.targetType == "movie" {
				return -1
			}
			return 1
		}
		return int(a.targetID - b.targetID)
	})
	return entries
}

func ptrString(s string) *string { return &s }
