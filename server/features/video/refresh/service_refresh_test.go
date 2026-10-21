package refresh

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// The scheduled refresh (D-025) is driven here without its loop: the selection
// queries directly, then runRefreshPass and runStatusSweep as the tick calls
// them. detailsService's clock is 2026-08-19T12:00Z and every seed helper
// stamps fetched_at 2026-08-18T00:00Z (36h old), so a 1h interval finds a seed
// due and a 7d one does not; ageSnapshot moves a row where a case needs it.

const (
	refreshOld   = "2026-08-01T00:00:00Z" // well past any interval under test
	refreshFresh = "2026-08-19T11:00:00Z" // an hour before the clock
	refreshWeek  = 7 * 24 * time.Hour
)

// ageSnapshot rewrites one catalog row's fetched_at. Tests are exempt from the
// "SQL only in store files" grep, and a helper here beats a store function
// nothing in production would call.
func ageSnapshot(t *testing.T, database *db.DB, table string, id int64, fetchedAt string) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE `+table+` SET fetched_at = ? WHERE id = ?`, fetchedAt, id)
		return err
	})
	if err != nil {
		t.Fatalf("ageing %s %d: %v", table, id, err)
	}
}

func movieIDs(refs []MovieRef) []int64 {
	out := make([]int64, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out
}

// newTMDBWith builds a test client over d. A nil d means "no credential" — the
// unconfigured client the tick must tolerate without fetching.
func newTMDBWith(d provider.Doer) *provider.TMDB {
	if d == nil {
		return provider.NewTMDB(provider.TMDBCredentials{})
	}
	return provider.NewTMDB(provider.TMDBCredentials{APIKeyV3: "test-key"}, provider.WithHTTPDoer(d))
}

func TestDueMovies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := videotest.DB(t)
	alice, bob := videotest.SeedUser(t, database, "alice"), videotest.SeedUser(t, database, "bob")
	at := "2026-08-10T20:00:00Z"

	watchlisted := videotest.SeedMovie(t, database, 1, "Watchlisted", nil, nil, "{}")
	videotest.SeedWatchlist(t, database, bob, "movie", watchlisted.ID, at) // another user's list counts
	watched := videotest.SeedMovie(t, database, 2, "Watched", nil, nil, "{}")
	videotest.SeedWatch(t, database, alice, "movie", watched.ID, at)
	both := videotest.SeedMovie(t, database, 3, "Watched and listed", nil, nil, "{}")
	videotest.SeedWatch(t, database, alice, "movie", both.ID, at)
	videotest.SeedWatchlist(t, database, alice, "movie", both.ID, at)
	rated := videotest.SeedMovie(t, database, 4, "Rated only", nil, nil, "{}")
	if err := database.InTx(ctx, func(tx db.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO user_video_ratings (user_id, target_type, target_id, rating, updated_at)
			VALUES (?, 'movie', ?, 8, ?)`, alice, rated.ID, at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	fresh := videotest.SeedMovie(t, database, 5, "Fresh", nil, nil, "{}")
	videotest.SeedWatchlist(t, database, alice, "movie", fresh.ID, at)
	videotest.SeedWatch(t, database, alice, "movie", fresh.ID, at)
	ageSnapshot(t, database, "movies", fresh.ID, refreshFresh)
	// D-013: an EPISODE watch and a show watchlist row with a movie's id are
	// not references to the movie.
	stranger := videotest.SeedMovie(t, database, 6, "Stranger", nil, nil, "{}")
	videotest.SeedWatch(t, database, alice, "episode", stranger.ID, at)
	videotest.SeedWatchlist(t, database, alice, "show", stranger.ID, at)

	cutoff := "2026-08-19T00:00:00Z"
	tests := []struct {
		class refreshClass
		want  []int64
	}{
		{RefreshActive, []int64{watchlisted.ID, both.ID}},
		{RefreshArchive, []int64{watched.ID}},
	}
	for _, tt := range tests {
		got, err := DueMovies(ctx, database, tt.class, cutoff)
		if err != nil {
			t.Fatalf("dueMovies(%s): %v", tt.class, err)
		}
		if ids := movieIDs(got); !slices.Equal(ids, tt.want) {
			t.Errorf("dueMovies(%s) = %v, want %v", tt.class, ids, tt.want)
		}
	}
}

func TestDueShows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := videotest.DB(t)
	alice, bob := videotest.SeedUser(t, database, "alice"), videotest.SeedUser(t, database, "bob")
	at := "2026-08-10T20:00:00Z"
	returning, ended, canceled := videotest.Ptr("Returning Series"), videotest.Ptr("Ended"), videotest.Ptr("Canceled")

	// Name → expected class ("" = neither).
	type fixture struct {
		name   string
		status *string
		state  string // alice's user_show_states status, "" for none
		listed bool   // on bob's watchlist
		fresh  bool
		want   refreshClass
	}
	fixtures := []fixture{
		{"watching, returning", returning, "watching", false, false, RefreshActive},
		{"watched, unknown production status", nil, "watched", false, false, RefreshActive},
		{"watchlisted ended show", ended, "", true, false, RefreshActive},
		{"watchlisted and dropped", ended, "dropped", true, false, RefreshActive},
		{"watching, ended", ended, "watching", false, false, RefreshArchive},
		{"watched, canceled", canceled, "watched", false, false, RefreshArchive},
		{"dropped only, returning", returning, "dropped", false, false, ""},
		{"dropped only, ended", ended, "dropped", false, false, ""},
		{"nobody follows it", returning, "", false, false, ""},
		{"fresh snapshot", returning, "watching", true, true, ""},
	}
	want := map[refreshClass][]int64{}
	for i, f := range fixtures {
		id := videotest.SeedShow(t, database, int64(100+i), f.name)
		videotest.SetShowProductionStatus(t, database, id, f.status)
		if f.state != "" {
			videotest.SeedShowStatus(t, database, alice, id, f.state)
		}
		if f.listed {
			videotest.SeedWatchlist(t, database, bob, "show", id, at)
		}
		if f.fresh {
			ageSnapshot(t, database, "shows", id, refreshFresh)
		}
		if f.want != "" {
			want[f.want] = append(want[f.want], id)
		}
	}

	cutoff := "2026-08-19T00:00:00Z"
	for _, class := range []refreshClass{RefreshActive, RefreshArchive} {
		got, err := DueShows(ctx, database, class, cutoff)
		if err != nil {
			t.Fatalf("dueShows(%s): %v", class, err)
		}
		if ids := videotest.ShowIDs(got); !slices.Equal(ids, want[class]) {
			t.Errorf("dueShows(%s) = %v, want %v", class, ids, want[class])
		}
	}
}

func TestDueOpenSeasons(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := videotest.DB(t)
	showID := videotest.SeedShow(t, database, 1396, "Breaking Bad")
	today := "2026-08-19"

	// Season 0: one undated special. Season 1: closed. Season 2: an episode
	// still to come. Season 3: an undated episode. Season 4: closed but the
	// highest. Season 5 would be open but was refreshed an hour ago.
	videotest.SeedSeasonSnapshot(t, database, showID, 1396, 0, nil, "{}")
	videotest.SeedEpisode(t, database, showID, 0, 1, nil)
	videotest.SeedSeasonSnapshot(t, database, showID, 1396, 1, nil, "{}")
	videotest.SeedEpisode(t, database, showID, 1, 1, videotest.Ptr("2008-01-20"))
	videotest.SeedSeasonSnapshot(t, database, showID, 1396, 2, nil, "{}")
	videotest.SeedEpisode(t, database, showID, 2, 1, videotest.Ptr("2026-08-01"))
	videotest.SeedEpisode(t, database, showID, 2, 2, videotest.Ptr("2026-09-01"))
	videotest.SeedSeasonSnapshot(t, database, showID, 1396, 3, nil, "{}")
	videotest.SeedEpisode(t, database, showID, 3, 1, nil)
	videotest.SeedSeasonSnapshot(t, database, showID, 1396, 4, nil, "{}")
	videotest.SeedEpisode(t, database, showID, 4, 1, videotest.Ptr("2010-01-01"))

	got, err := dueOpenSeasons(ctx, database, showID, "2026-08-19T00:00:00Z", today)
	if err != nil {
		t.Fatalf("dueOpenSeasons: %v", err)
	}
	if want := []int{2, 3, 4}; len(got) != 3 || got[0] != 2 || got[1] != 3 || got[2] != 4 {
		t.Errorf("open seasons = %v, want %v", got, want)
	}

	// A fresh open season is not due; the highest season can also be one that
	// was just written, so it drops out the same way.
	five := videotest.SeedSeasonSnapshot(t, database, showID, 1396, 5, nil, "{}")
	videotest.SeedEpisode(t, database, showID, 5, 1, nil)
	ageSnapshot(t, database, "seasons", five, refreshFresh)
	got, err = dueOpenSeasons(ctx, database, showID, "2026-08-19T00:00:00Z", today)
	if err != nil {
		t.Fatalf("dueOpenSeasons: %v", err)
	}
	// Season 4 is no longer the highest and is closed, so it leaves too.
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("open seasons after a fresh season 5 = %v, want [2 3]", got)
	}

	// Another show's seasons never leak in.
	other := videotest.SeedShow(t, database, 2000, "Other")
	videotest.SeedSeasonSnapshot(t, database, other, 2000, 1, nil, "{}")
	got, err = dueOpenSeasons(ctx, database, other, "2026-08-19T00:00:00Z", today)
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Errorf("other show's open seasons = %v, %v; want [1]", got, err)
	}
}

// refreshService is detailsService with the two intervals set so the seeds
// (36h old) are due for both classes.
func refreshService(t *testing.T, stub *videotest.ProviderStub) (*Service, *time.Time) {
	t.Helper()
	svc, clock := detailsService(t, stub)
	svc.refresh.active, svc.refresh.archive = time.Hour, time.Hour
	return svc, clock
}

func TestRunRefreshPass(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	at := "2026-08-10T20:00:00Z"

	t.Run("a due movie is refetched and a fresh one left alone", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := refreshService(t, stub)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		due := videotest.SeedMovie(t, svc.DB, 603, "Old title", nil, nil, "{}")
		videotest.SeedWatchlist(t, svc.DB, alice, "movie", due.ID, at)
		fresh := videotest.SeedMovie(t, svc.DB, 604, "Fresh", nil, nil, "{}")
		videotest.SeedWatchlist(t, svc.DB, alice, "movie", fresh.ID, at)
		ageSnapshot(t, svc.DB, "movies", fresh.ID, refreshFresh)

		var breaker common.ProviderBreaker
		rep := svc.runRefreshPass(ctx, RefreshActive, time.Hour, &breaker)
		if rep.Movies != 1 || rep.Failed != 0 || rep.Aborted {
			t.Fatalf("report = %+v, want one movie refreshed", rep)
		}
		row, err := common.GetMovieByID(ctx, svc.DB, due.ID)
		if err != nil || row.Title != "The Matrix" || row.FetchedAt != "2026-08-19T12:00:00Z" {
			t.Errorf("row after refresh = %+v (%v), want the stub's title and the clock's fetched_at", row, err)
		}
		if stub.CallCount("/3/movie/604") != 0 {
			t.Error("the fresh movie was fetched")
		}
	})

	t.Run("a class whose interval is zero makes no call", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := refreshService(t, stub)
		svc.refresh.archive = 0
		alice := videotest.SeedUser(t, svc.DB, "alice")
		m := videotest.SeedMovie(t, svc.DB, 603, "Watched", nil, nil, "{}")
		videotest.SeedWatch(t, svc.DB, alice, "movie", m.ID, at)

		svc.runRefreshTick(ctx)
		if stub.CallCount("/3/movie/603") != 0 {
			t.Error("the archive job ran with a zero interval")
		}
	})

	t.Run("the archive class refreshes an ended show on its own cutoff", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithOneSeasonBody}}
		svc, _ := refreshService(t, stub)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		showID := videotest.SeedShow(t, svc.DB, 1396, "Old title")
		videotest.SetShowProductionStatus(t, svc.DB, showID, videotest.Ptr("Ended"))
		videotest.SeedShowStatus(t, svc.DB, alice, showID, "watched")
		ageSnapshot(t, svc.DB, "shows", showID, refreshOld)

		// Active: a week — not due. Archive: an hour — due.
		var breaker common.ProviderBreaker
		if rep := svc.runRefreshPass(ctx, RefreshActive, refreshWeek, &breaker); rep.Shows != 0 {
			t.Fatalf("active pass = %+v, want nothing (ended shows are archive)", rep)
		}
		rep := svc.runRefreshPass(ctx, RefreshArchive, time.Hour, &breaker)
		if rep.Shows != 1 || rep.Failed != 0 {
			t.Fatalf("archive pass = %+v, want one show", rep)
		}
		show, err := common.GetShowByID(ctx, svc.DB, showID)
		if err != nil || show.Title != "Breaking Bad" {
			t.Errorf("show after refresh = %+v (%v)", show, err)
		}
		// The root refresh materialised season 1, which the show had no row for.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes WHERE show_id = ?`, showID); n != 1 {
			t.Errorf("episodes = %d, want the materialised season's 1", n)
		}
	})

	t.Run("open seasons are refetched, closed ones are not, and the sweep demotes", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := refreshService(t, stub)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		showID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		videotest.SeedShowStatus(t, svc.DB, alice, showID, "watched")
		// Season 1 closed and watched; season 2 known but with one undated
		// episode, as TMDB lists a season before dating it.
		videotest.SeedSeasonSnapshot(t, svc.DB, showID, 1396, 1, nil, `{"episodes": []}`)
		s1e1 := videotest.SeedEpisode(t, svc.DB, showID, 1, 1, videotest.Ptr("2008-01-20"))
		videotest.SeedWatch(t, svc.DB, alice, "episode", s1e1, at)
		videotest.SeedSeasonSnapshot(t, svc.DB, showID, 1396, 2, nil, `{"episodes": []}`)
		videotest.SeedEpisode(t, svc.DB, showID, 2, 1, nil)

		var breaker common.ProviderBreaker
		rep := svc.runRefreshPass(ctx, RefreshActive, time.Hour, &breaker)
		if rep.Shows != 1 || rep.Seasons != 1 || rep.Failed != 0 {
			t.Fatalf("report = %+v, want one show and one (open) season", rep)
		}
		// Root + season 2 = two calls against the one path; season 1 was closed.
		if n := stub.CallCount("/3/tv/1396"); n != 2 {
			t.Errorf("tv calls = %d, want 2 (root, season 2)", n)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes WHERE show_id = ? AND season_number = 2`, showID); n != 4 {
			t.Errorf("season 2 episodes = %d, want the fixture's 4", n)
		}
		// Nothing in the pass touched the status; the sweep does.
		if got := videotest.ShowStatus(t, svc.DB, alice, showID); got != "watched" {
			t.Fatalf("status after the pass = %q, want still watched", got)
		}
		n, err := svc.runStatusSweep(ctx)
		if err != nil || n != 1 {
			t.Fatalf("sweep = %d, %v; want 1 pair", n, err)
		}
		if got := videotest.ShowStatus(t, svc.DB, alice, showID); got != "watching" {
			t.Errorf("status after the sweep = %q, want watching — season 2 aired", got)
		}
	})

	t.Run("a root that fails skips its seasons and keeps the snapshot", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/tv/1396": errors.New("dial tcp: connection refused")}}
		svc, _ := refreshService(t, stub)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		showID := videotest.SeedShow(t, svc.DB, 1396, "Old title")
		videotest.SeedShowStatus(t, svc.DB, alice, showID, "watching")
		videotest.SeedSeasonSnapshot(t, svc.DB, showID, 1396, 1, nil, `{"episodes": []}`)
		videotest.SeedEpisode(t, svc.DB, showID, 1, 1, nil)

		var breaker common.ProviderBreaker
		rep := svc.runRefreshPass(ctx, RefreshActive, time.Hour, &breaker)
		if rep.Shows != 0 || rep.Seasons != 0 || rep.Failed != 1 {
			t.Fatalf("report = %+v, want one failure and no season fetch", rep)
		}
		if n := stub.CallCount("/3/tv/1396"); n != 1 {
			t.Errorf("tv calls = %d, want the root only", n)
		}
		show, err := common.GetShowByID(ctx, svc.DB, showID)
		if err != nil || show.Title != "Old title" || show.FetchedAt != "2026-08-18T00:00:00Z" {
			t.Errorf("show = %+v (%v), want the snapshot untouched", show, err)
		}
	})

	t.Run("the breaker ends the pass after three outages", func(t *testing.T) {
		t.Parallel()
		down := errors.New("dial tcp: connection refused")
		stub := &videotest.ProviderStub{
			Errs:   map[string]error{"/3/movie/1": down, "/3/movie/2": down, "/3/movie/3": down},
			Bodies: map[string]string{"/3/movie/4": videotest.MatrixDetailsBody},
		}
		svc, _ := refreshService(t, stub)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		for i := int64(1); i <= 4; i++ {
			m := videotest.SeedMovie(t, svc.DB, i, "M", nil, nil, "{}")
			videotest.SeedWatchlist(t, svc.DB, alice, "movie", m.ID, at)
		}

		var breaker common.ProviderBreaker
		rep := svc.runRefreshPass(ctx, RefreshActive, time.Hour, &breaker)
		if !rep.Aborted || rep.Failed != 3 || rep.Movies != 0 {
			t.Fatalf("report = %+v, want aborted after three failures", rep)
		}
		if stub.CallCount("/3/movie/4") != 0 {
			t.Error("the fourth movie was fetched after the breaker tripped")
		}
		// The tick shares the breaker, so the archive pass is skipped too.
		if !breaker.Open() {
			t.Error("breaker closed again")
		}
	})

	t.Run("a 404 keeps the row and silences the item for one interval", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{} // nothing scripted: every path is a 404
		svc, clock := refreshService(t, stub)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		gone := videotest.SeedMovie(t, svc.DB, 603, "Removed", nil, nil, "{}")
		videotest.SeedWatchlist(t, svc.DB, alice, "movie", gone.ID, at)

		var breaker common.ProviderBreaker
		rep := svc.runRefreshPass(ctx, RefreshActive, time.Hour, &breaker)
		if rep.Failed != 1 || rep.Aborted {
			t.Fatalf("first pass = %+v, want one failure, not aborted (a 404 is not an outage)", rep)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM movies WHERE id = ?`, gone.ID); n != 1 {
			t.Fatal("the movie row was deleted")
		}
		rep = svc.runRefreshPass(ctx, RefreshActive, time.Hour, &breaker)
		if rep.Skipped != 1 || stub.CallCount("/3/movie/603") != 1 {
			t.Errorf("second pass = %+v with %d calls, want the item skipped without a call", rep, stub.CallCount("/3/movie/603"))
		}
		// After the interval the memory expires and TMDB is asked again.
		*clock = clock.Add(2 * time.Hour)
		rep = svc.runRefreshPass(ctx, RefreshActive, time.Hour, &breaker)
		if rep.Skipped != 0 || stub.CallCount("/3/movie/603") != 2 {
			t.Errorf("third pass = %+v with %d calls, want a retry", rep, stub.CallCount("/3/movie/603"))
		}
	})

	t.Run("a cancelled context ends the pass between items", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/1": videotest.MatrixDetailsBody, "/3/movie/2": videotest.MatrixDetailsBody}}
		svc, _ := refreshService(t, stub)
		cctx, cancel := context.WithCancel(ctx)
		svc.TMDB = newTMDBWith(&videotest.CancelAfter{Inner: stub, Cancel: cancel})
		alice := videotest.SeedUser(t, svc.DB, "alice")
		for i := int64(1); i <= 2; i++ {
			m := videotest.SeedMovie(t, svc.DB, i, "M", nil, nil, "{}")
			videotest.SeedWatchlist(t, svc.DB, alice, "movie", m.ID, at)
		}

		var breaker common.ProviderBreaker
		rep := svc.runRefreshPass(cctx, RefreshActive, time.Hour, &breaker)
		// The first item is either written or, if the cancel landed before its
		// transaction, counted as failed; either way exactly one was attempted.
		if !rep.Aborted || rep.Movies+rep.Failed != 1 {
			t.Fatalf("report = %+v, want one attempt then an abort", rep)
		}
		if stub.CallCount("/3/movie/2") != 0 {
			t.Error("the second movie was fetched after cancellation")
		}
	})
}

func TestRunStatusSweep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	at := "2026-08-10T20:00:00Z"

	t.Run("midnight passing flips a show with no fetch at all", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, clock := refreshService(t, stub)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		showID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		s1e1 := videotest.SeedEpisode(t, svc.DB, showID, 1, 1, videotest.Ptr("2008-01-20"))
		videotest.SeedEpisode(t, svc.DB, showID, 1, 2, videotest.Ptr("2026-08-20")) // tomorrow, by the clock
		videotest.SeedWatch(t, svc.DB, alice, "episode", s1e1, at)
		videotest.SeedShowStatus(t, svc.DB, alice, showID, "watched")

		if n, err := svc.runStatusSweep(ctx); err != nil || n != 1 {
			t.Fatalf("sweep = %d, %v", n, err)
		}
		if got := videotest.ShowStatus(t, svc.DB, alice, showID); got != "watched" {
			t.Fatalf("status before the air date = %q, want watched", got)
		}
		*clock = clock.Add(24 * time.Hour)
		if n, err := svc.runStatusSweep(ctx); err != nil || n != 1 {
			t.Fatalf("sweep = %d, %v", n, err)
		}
		if got := videotest.ShowStatus(t, svc.DB, alice, showID); got != "watching" {
			t.Errorf("status after the air date = %q, want watching", got)
		}
		if len(stub.Calls) != 0 {
			t.Errorf("the sweep reached TMDB: %v", stub.Calls)
		}
	})

	// D-038: each pair is swept against its own user's midnight. At 22:30
	// UTC an episode dated the next day has aired in Bucharest, so the same
	// sweep moves one follower and leaves the other.
	t.Run("each user's day ends at their own midnight", func(t *testing.T) {
		t.Parallel()
		svc, clock := refreshService(t, &videotest.ProviderStub{})
		*clock = videotest.LateEveningClock()
		ro, ny := videotest.SeedUser(t, svc.DB, "ro"), videotest.SeedUser(t, svc.DB, "ny")
		videotest.SetUserTimezone(t, svc.DB, ro, "Europe/Bucharest")
		videotest.SetUserTimezone(t, svc.DB, ny, "America/New_York")
		showID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		s1e1 := videotest.SeedEpisode(t, svc.DB, showID, 1, 1, videotest.Ptr("2008-01-20"))
		videotest.SeedEpisode(t, svc.DB, showID, 1, 2, videotest.Ptr("2026-08-19"))
		for _, u := range []int64{ro, ny} {
			videotest.SeedWatch(t, svc.DB, u, "episode", s1e1, at)
			videotest.SeedShowStatus(t, svc.DB, u, showID, "watched")
		}

		if n, err := svc.runStatusSweep(ctx); err != nil || n != 2 {
			t.Fatalf("sweep = %d, %v", n, err)
		}
		if got := videotest.ShowStatus(t, svc.DB, ro, showID); got != "watching" {
			t.Errorf("Bucharest = %q, want watching — S1E2 aired at their midnight", got)
		}
		if got := videotest.ShowStatus(t, svc.DB, ny, showID); got != "watched" {
			t.Errorf("New York = %q, want watched — S1E2 airs tomorrow there", got)
		}
	})

	t.Run("a dropped show stays dropped and an orphan watch regains its row", func(t *testing.T) {
		t.Parallel()
		svc, _ := refreshService(t, &videotest.ProviderStub{})
		alice, bob := videotest.SeedUser(t, svc.DB, "alice"), videotest.SeedUser(t, svc.DB, "bob")
		showID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		s1e1 := videotest.SeedEpisode(t, svc.DB, showID, 1, 1, videotest.Ptr("2008-01-20"))
		videotest.SeedWatch(t, svc.DB, alice, "episode", s1e1, at)
		videotest.SeedShowStatus(t, svc.DB, alice, showID, "dropped")
		videotest.SeedWatch(t, svc.DB, bob, "episode", s1e1, at) // no state row: the repair case

		n, err := svc.runStatusSweep(ctx)
		if err != nil || n != 1 {
			t.Fatalf("sweep = %d, %v; want bob's pair only (alice's is dropped)", n, err)
		}
		if got := videotest.ShowStatus(t, svc.DB, alice, showID); got != "dropped" {
			t.Errorf("alice = %q, want dropped (sticky)", got)
		}
		if got := videotest.ShowStatus(t, svc.DB, bob, showID); got != "watched" {
			t.Errorf("bob = %q, want watched — the one aired episode is watched", got)
		}
	})
}

func TestRunRefreshTick(t *testing.T) {
	t.Parallel()

	t.Run("without a TMDB credential the sweep still runs", func(t *testing.T) {
		t.Parallel()
		svc, _ := refreshService(t, &videotest.ProviderStub{})
		svc.TMDB = newTMDBWith(nil) // no credential → Configured() is false
		alice := videotest.SeedUser(t, svc.DB, "alice")
		showID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		s1e1 := videotest.SeedEpisode(t, svc.DB, showID, 1, 1, videotest.Ptr("2008-01-20"))
		videotest.SeedWatch(t, svc.DB, alice, "episode", s1e1, "2026-08-10T20:00:00Z")
		videotest.SeedShowStatus(t, svc.DB, alice, showID, "watching")

		svc.runRefreshTick(context.Background())
		if got := videotest.ShowStatus(t, svc.DB, alice, showID); got != "watched" {
			t.Errorf("status = %q, want the sweep to have promoted it", got)
		}
	})

	t.Run("the loop stops when its context ends", func(t *testing.T) {
		t.Parallel()
		svc, _ := refreshService(t, &videotest.ProviderStub{})
		svc.refresh.bootDelay, svc.refresh.tick = time.Millisecond, time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			svc.Run(ctx)
			close(done)
		}()
		time.Sleep(20 * time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancellation")
		}
	})
}

// A provider error other than 404 must never be mistaken for one by the pass.
func TestProviderBreakerIgnoresNotFound(t *testing.T) {
	t.Parallel()
	var b common.ProviderBreaker
	for range common.ProviderBreakerTrips {
		b.Observe(httpx.ErrNotFound)
	}
	if b.Open() {
		t.Error("404s tripped the breaker")
	}
}
