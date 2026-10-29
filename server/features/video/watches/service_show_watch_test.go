package watches

import (
	"context"
	"errors"
	"testing"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/tz"
)

// trackedShowService materialises Breaking Bad and returns the service, the
// user and the show row — the starting point for every marking test.
func trackedShowService(t *testing.T) (*Service, int64, common.ShowRow) {
	t.Helper()
	stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
	svc, _ := detailsService(t, stub)
	userID := videotest.SeedUser(t, svc.DB, "alice")
	show, err := svc.MaterialiseShow(context.Background(), 1396)
	if err != nil {
		t.Fatalf("materialiseShow: %v", err)
	}
	return svc, userID, show
}

func TestMarkEpisodeWatched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("marking an episode of an untracked show materialises the whole show", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{
			TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1),
		}))
		if err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		if !res.Created || res.TargetType != "episode" || res.TargetID < 1 {
			t.Fatalf("result = %+v", res)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes`); n != 5 {
			t.Errorf("episodes = %d, want 5 — marking one episode must pull the whole show local", n)
		}
		// The point of the feature: the show is now watching.
		if res.Show == nil || res.Show.Status == nil || *res.Show.Status != "watching" {
			t.Fatalf("res.Show = %+v, want status watching", res.Show)
		}
		// Season 2 episode 2 is undated in the fixture, so it is not aired and
		// does not count toward the denominator.
		if res.Show.Progress.Watched != 1 {
			t.Errorf("progress.watched = %d, want 1", res.Show.Progress.Watched)
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		p := common.TrackParams{TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1)}

		if _, err := svc.MarkWatched(ctx, userID, watchNow(p)); err != nil {
			t.Fatalf("first MarkWatched: %v", err)
		}
		again, err := svc.MarkWatched(ctx, userID, watchNow(p))
		if err != nil {
			t.Fatalf("second MarkWatched: %v", err)
		}
		if again.Created {
			t.Error("second mark reported created — a rewatch needs an explicit watched_at (D-009)")
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 1 {
			t.Errorf("watch rows = %d, want 1", n)
		}
	})

	t.Run("an episode is addressable by its local id once tracked", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		ep, err := common.GetEpisodeByShowKey(ctx, svc.DB, show.ID, 2, 1)
		if err != nil {
			t.Fatalf("getEpisodeByShowKey: %v", err)
		}
		res, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{TargetType: "episode", TargetID: &ep.ID}))
		if err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		if res.TargetID != ep.ID {
			t.Errorf("target id = %d, want %d", res.TargetID, ep.ID)
		}
	})

	// The one thing backdating must NOT move: D-012's aired-episode predicate.
	// Every episode in the fixture aired in 2008-2009, so a watch logged for
	// 2007 would leave nothing "aired" if the recompute took its day from the
	// backdate — the show would read watched against a denominator of zero.
	t.Run("a backdated log still counts aired episodes against the real clock", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)

		res, err := svc.MarkWatched(ctx, userID, watchAt(common.TrackParams{
			TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1),
		}, "2007-01-01T00:00:00Z"))
		if err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		if res.Show == nil || res.Show.Progress.AiredTotal != 4 {
			t.Fatalf("res.Show = %+v, want aired_total 4", res.Show)
		}
		if res.Show.Status == nil || *res.Show.Status != "watching" {
			t.Fatalf("status = %v, want watching", res.Show.Status)
		}
	})

	t.Run("bad episode addressing", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		tests := []struct {
			name string
			p    common.TrackParams
			want error
		}{
			{"no season or episode alongside tmdb_id",
				common.TrackParams{TargetType: "episode", TMDBID: videotest.PtrInt64(1396)}, httpx.ErrValidation},
			{"season and episode alongside a local id",
				common.TrackParams{TargetType: "episode", TargetID: videotest.PtrInt64(1), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1)},
				httpx.ErrValidation},
			{"season on a movie target",
				common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603), Season: videotest.PtrInt(1)}, httpx.ErrValidation},
			{"negative episode",
				common.TrackParams{TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(-1)},
				httpx.ErrValidation},
			{"an episode the show does not have",
				common.TrackParams{TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(99)},
				httpx.ErrNotFound},
			{"an unknown local episode id",
				common.TrackParams{TargetType: "episode", TargetID: videotest.PtrInt64(9999)}, httpx.ErrNotFound},
			// Marking a whole show is a bulk operation with a count for an
			// answer, so it is refused here rather than guessed at.
			{"a show target", common.TrackParams{TargetType: "show", TMDBID: videotest.PtrInt64(1396)}, httpx.ErrValidation},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := svc.MarkWatched(ctx, userID, watchNow(tc.p)); !errors.Is(err, tc.want) {
					t.Errorf("error = %v, want %v", err, tc.want)
				}
			})
		}
	})
}

func TestBulkMarkWatched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// The fixture has 5 episodes: S1E1, and S2E1-E4. S2E2 is undated, so four
	// are aired and non-special.
	const airedNonSpecials = 4

	t.Run("marks every aired non-special and completes the show", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)

		res, err := svc.BulkMarkWatched(ctx, userID, BulkWatchParams{
			Scope: "show", TrackParams: common.TrackParams{TargetType: "show", TMDBID: videotest.PtrInt64(1396)},
		})
		if err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		if res.Created != airedNonSpecials {
			t.Errorf("created = %d, want %d", res.Created, airedNonSpecials)
		}
		if res.Status == nil || *res.Status != "watched" {
			t.Fatalf("status = %v, want watched", res.Status)
		}
		if res.Progress.Watched != airedNonSpecials || res.Progress.AiredTotal != airedNonSpecials {
			t.Errorf("progress = %+v, want %d/%d", res.Progress, airedNonSpecials, airedNonSpecials)
		}
		// Total counts non-specials whether aired or not, so the undated S2E2
		// is in it — which is why showing watched/aired and total separately
		// matters.
		if res.Progress.Total != 5 {
			t.Errorf("progress.total = %d, want 5", res.Progress.Total)
		}
		if res.TargetID != show.ID {
			t.Errorf("target id = %d, want %d", res.TargetID, show.ID)
		}

		// One watched_at across the batch — the (watched_at, id) feed tie-break
		// exists for exactly this.
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(DISTINCT watched_at) FROM user_watch_history`); n != 1 {
			t.Errorf("distinct watched_at = %d, want 1", n)
		}
	})

	// The most likely bug in the whole design: without the insert's NOT EXISTS,
	// a double tap is a few hundred rows indistinguishable from real rewatches.
	t.Run("running it twice writes nothing the second time", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		p := BulkWatchParams{Scope: "show", TrackParams: common.TrackParams{TargetType: "show", TMDBID: videotest.PtrInt64(1396)}}

		if _, err := svc.BulkMarkWatched(ctx, userID, p); err != nil {
			t.Fatalf("first BulkMarkWatched: %v", err)
		}
		again, err := svc.BulkMarkWatched(ctx, userID, p)
		if err != nil {
			t.Fatalf("second BulkMarkWatched: %v", err)
		}
		if again.Created != 0 {
			t.Errorf("second call created %d rows, want 0", again.Created)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != airedNonSpecials {
			t.Errorf("watch rows = %d, want %d", n, airedNonSpecials)
		}
	})

	t.Run("another user's marks are untouched", func(t *testing.T) {
		t.Parallel()
		svc, alice, show := trackedShowService(t)
		bob := videotest.SeedUser(t, svc.DB, "bob")

		if _, err := svc.BulkMarkWatched(ctx, alice, BulkWatchParams{
			Scope: "show", TrackParams: common.TrackParams{TargetType: "show", TargetID: &show.ID},
		}); err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		p, err := common.ShowProgress(ctx, svc.DB, bob, show.ID, "2026-08-19")
		if err != nil {
			t.Fatalf("showProgress: %v", err)
		}
		if p.Watched != 0 {
			t.Errorf("bob's watched = %d, want 0", p.Watched)
		}
	})

	t.Run("a bad scope is a validation error", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		for _, scope := range []string{"", "episode", "all"} {
			_, err := svc.BulkMarkWatched(ctx, userID, BulkWatchParams{
				Scope: scope, TrackParams: common.TrackParams{TargetType: "show", TMDBID: videotest.PtrInt64(1396)},
			})
			if !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("scope %q → error %v, want ErrValidation", scope, err)
			}
		}
	})

	// The mode/rewatch axes. bulkShow builds the common body: the fixture show,
	// addressed by tmdb_id.
	bulkShow := func(mode string, at *string, rewatch bool) BulkWatchParams {
		return BulkWatchParams{
			Scope:       "show",
			TrackParams: common.TrackParams{TargetType: "show", TMDBID: videotest.PtrInt64(1396)},
			Rewatch:     rewatch, WatchedAtMode: mode, WatchedAt: at,
		}
	}
	strPtr := func(s string) *string { return &s }

	// The one thing backdating must NOT move, bulk edition: the recompute's
	// aired predicate keeps the real clock, so a batch dated 2007 — before any
	// fixture episode aired — still completes the show.
	t.Run("a fixed backdate stamps every row and still completes the show", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)

		res, err := svc.BulkMarkWatched(ctx, userID, bulkShow("fixed", strPtr("2007-05-05T10:30:00+02:00"), false))
		if err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		if res.Created != airedNonSpecials {
			t.Errorf("created = %d, want %d", res.Created, airedNonSpecials)
		}
		if res.Status == nil || *res.Status != "watched" {
			t.Fatalf("status = %v, want watched — the recompute must use the real clock", res.Status)
		}
		// The offset is normalised away on store, like the single log's.
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_watch_history WHERE watched_at = '2007-05-05T08:30:00Z'`); n != airedNonSpecials {
			t.Errorf("rows at the normalised instant = %d, want %d", n, airedNonSpecials)
		}
	})

	t.Run("episode_air dates each row from its own air date", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)

		res, err := svc.BulkMarkWatched(ctx, userID, bulkShow("episode_air", nil, false))
		if err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		if res.Created != airedNonSpecials {
			t.Errorf("created = %d, want %d", res.Created, airedNonSpecials)
		}
		// Every row's instant is exactly its episode's air date at midnight UTC
		// — fixed-width, or the feed's plain-string ordering breaks.
		if n := videotest.CountRows(t, svc.DB, `
SELECT COUNT(*) FROM user_watch_history wh
JOIN episodes e ON e.id = wh.target_id
WHERE wh.target_type = 'episode' AND wh.watched_at = e.air_date || 'T00:00:00Z'`); n != airedNonSpecials {
			t.Errorf("rows matching their episode's air date = %d, want %d", n, airedNonSpecials)
		}
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(DISTINCT watched_at) FROM user_watch_history`); n != airedNonSpecials {
			t.Errorf("distinct watched_at = %d, want %d — the fixture's aired episodes all differ", n, airedNonSpecials)
		}
	})

	// D-038: "on its air date" is midnight where the user lives, stored as
	// its UTC instant — for Bucharest the evening before in UTC, which is
	// what renders as 00:00 on the air date in their zone.
	t.Run("episode_air stamps midnight in the user's zone", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		videotest.SetUserTimezone(t, svc.DB, userID, "Europe/Bucharest")
		bucharest, err := tz.Load("Europe/Bucharest")
		if err != nil {
			t.Fatal(err)
		}

		if _, err := svc.BulkMarkWatched(ctx, userID, bulkShow("episode_air", nil, false)); err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		rows, err := svc.DB.Read(ctx, `
SELECT wh.watched_at, e.air_date FROM user_watch_history wh
JOIN episodes e ON e.id = wh.target_id AND wh.target_type = 'episode'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var watchedAt, airDate string
			if err := rows.Scan(&watchedAt, &airDate); err != nil {
				t.Fatal(err)
			}
			want, _ := tz.Midnight(airDate, bucharest)
			if watchedAt != want || len(watchedAt) != len("2006-01-02T15:04:05Z") {
				t.Errorf("air date %s: watched_at = %q, want %q", airDate, watchedAt, want)
			}
			if watchedAt[:10] == airDate {
				t.Errorf("air date %s: watched_at %q is UTC midnight, want Bucharest's", airDate, watchedAt)
			}
			n++
		}
		if n != airedNonSpecials {
			t.Errorf("rows = %d, want %d", n, airedNonSpecials)
		}
	})

	t.Run("season_air dates each row from its season premiere", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)

		res, err := svc.BulkMarkWatched(ctx, userID, bulkShow("season_air", nil, false))
		if err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		if res.Created != airedNonSpecials {
			t.Errorf("created = %d, want %d", res.Created, airedNonSpecials)
		}
		for _, want := range []struct {
			season int
			at     string
			rows   int
		}{
			{1, "2008-01-20T00:00:00Z", 1},
			{2, "2009-03-08T00:00:00Z", 3},
		} {
			n := videotest.CountRows(t, svc.DB, `
SELECT COUNT(*) FROM user_watch_history wh
JOIN episodes e ON e.id = wh.target_id
WHERE wh.target_type = 'episode' AND e.season_number = ? AND wh.watched_at = ?`,
				want.season, want.at)
			if n != want.rows {
				t.Errorf("season %d rows at %s = %d, want %d", want.season, want.at, n, want.rows)
			}
		}
	})

	t.Run("season_air without a premiere needs the fallback", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			_, txErr := tx.Exec(ctx, `UPDATE seasons SET air_date = NULL WHERE season_number = 2`)
			return txErr
		})
		if err != nil {
			t.Fatalf("nulling the premiere: %v", err)
		}

		// No fallback: refused, and the tx aborts before any row lands.
		if _, err := svc.BulkMarkWatched(ctx, userID, bulkShow("season_air", nil, false)); !errors.Is(err, httpx.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 0 {
			t.Fatalf("watch rows after the refusal = %d, want 0 — the whole tx must roll back", n)
		}

		// With one: the dated season keeps its premiere, the undated one takes
		// the fallback.
		res, err := svc.BulkMarkWatched(ctx, userID, bulkShow("season_air", strPtr("2010-01-01T20:00:00Z"), false))
		if err != nil {
			t.Fatalf("BulkMarkWatched with fallback: %v", err)
		}
		if res.Created != airedNonSpecials {
			t.Errorf("created = %d, want %d", res.Created, airedNonSpecials)
		}
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_watch_history WHERE watched_at = '2008-01-20T00:00:00Z'`); n != 1 {
			t.Errorf("season 1 rows at its premiere = %d, want 1", n)
		}
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_watch_history WHERE watched_at = '2010-01-01T20:00:00Z'`); n != 3 {
			t.Errorf("fallback-dated rows = %d, want 3", n)
		}
	})

	t.Run("rewatch always inserts, watched episodes included", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)

		// Partial progress first: one episode watched the ordinary way.
		if _, err := svc.MarkWatched(ctx, userID, watchNow(common.TrackParams{
			TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1),
		})); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}

		res, err := svc.BulkMarkWatched(ctx, userID, bulkShow("episode_air", nil, true))
		if err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		if res.Created != airedNonSpecials {
			t.Errorf("created = %d, want %d — a rewatch covers watched episodes too", res.Created, airedNonSpecials)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != airedNonSpecials+1 {
			t.Errorf("watch rows = %d, want %d", n, airedNonSpecials+1)
		}
		if res.Status == nil || *res.Status != "watched" {
			t.Fatalf("status = %v, want watched", res.Status)
		}
		// Progress counts episodes, never watch rows, so a rewatch cannot push
		// it past the aired total.
		if res.Progress.Watched != airedNonSpecials {
			t.Errorf("progress.watched = %d, want %d", res.Progress.Watched, airedNonSpecials)
		}
	})

	t.Run("bad mode and watched_at combinations", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		tests := []struct {
			name string
			p    BulkWatchParams
		}{
			{"unknown mode", bulkShow("air_date", nil, false)},
			{"fixed without watched_at", bulkShow("fixed", nil, false)},
			{"fixed with a future watched_at", bulkShow("fixed", strPtr("2036-01-01T00:00:00Z"), false)},
			{"fixed with a malformed watched_at", bulkShow("fixed", strPtr("yesterday"), false)},
			{"watched_at alongside now", bulkShow("now", strPtr("2020-01-01T00:00:00Z"), false)},
			{"watched_at alongside the default mode", bulkShow("", strPtr("2020-01-01T00:00:00Z"), false)},
			{"watched_at alongside episode_air", bulkShow("episode_air", strPtr("2020-01-01T00:00:00Z"), false)},
			{"a future season_air fallback", bulkShow("season_air", strPtr("2036-01-01T00:00:00Z"), false)},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := svc.BulkMarkWatched(ctx, userID, tc.p); !errors.Is(err, httpx.ErrValidation) {
					t.Errorf("error = %v, want ErrValidation", err)
				}
			})
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 0 {
			t.Errorf("watch rows after the refusals = %d, want 0", n)
		}
	})
}

func TestBulkMarkSeasonWatched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// The fixture's per-season eligible counts: season 1 has its one aired
	// episode; season 2 has three aired of four (E2 is undated).
	const s1Aired, s2Aired = 1, 3

	seasonScope := func(p common.TrackParams, mode string, at *string, rewatch bool) BulkWatchParams {
		return BulkWatchParams{
			Scope: "season", TrackParams: p,
			Rewatch: rewatch, WatchedAtMode: mode, WatchedAt: at,
		}
	}
	byKey := func(season int) common.TrackParams {
		return common.TrackParams{TargetType: "season", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(season)}
	}
	strPtr := func(s string) *string { return &s }

	t.Run("marks one season and no more", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)

		res, err := svc.BulkMarkWatched(ctx, userID, seasonScope(byKey(2), "", nil, false))
		if err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		if res.Created != s2Aired {
			t.Errorf("created = %d, want %d", res.Created, s2Aired)
		}
		season, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 2)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		if res.TargetType != "season" || res.TargetID != season.ID {
			t.Errorf("target = %s %d, want season %d", res.TargetType, res.TargetID, season.ID)
		}
		// The show is NOT complete: season 1 is untouched — the proof the mark
		// did not leak past the season. Status and progress are the show's.
		if res.Status == nil || *res.Status != "watching" {
			t.Fatalf("status = %v, want watching", res.Status)
		}
		if res.Progress.Watched != s2Aired || res.Progress.AiredTotal != s1Aired+s2Aired {
			t.Errorf("progress = %+v, want %d/%d", res.Progress, s2Aired, s1Aired+s2Aired)
		}
		if n := videotest.CountRows(t, svc.DB, `
SELECT COUNT(*) FROM user_watch_history wh
JOIN episodes e ON e.id = wh.target_id
WHERE wh.target_type = 'episode' AND e.season_number = 1`); n != 0 {
			t.Errorf("season 1 rows = %d, want 0", n)
		}
		_ = show
	})

	t.Run("a season target_id works, and the last season completes the show", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)

		if _, err := svc.BulkMarkWatched(ctx, userID, seasonScope(byKey(2), "", nil, false)); err != nil {
			t.Fatalf("season 2: %v", err)
		}
		season1, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 1)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		res, err := svc.BulkMarkWatched(ctx, userID, seasonScope(
			common.TrackParams{TargetType: "season", TargetID: &season1.ID}, "", nil, false))
		if err != nil {
			t.Fatalf("season 1 by id: %v", err)
		}
		if res.Created != s1Aired {
			t.Errorf("created = %d, want %d", res.Created, s1Aired)
		}
		if res.Status == nil || *res.Status != "watched" {
			t.Fatalf("status = %v, want watched — the last season completes the show", res.Status)
		}
	})

	t.Run("is idempotent without rewatch, and rewatch stays inside the season", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		p := seasonScope(byKey(2), "", nil, false)

		if _, err := svc.BulkMarkWatched(ctx, userID, p); err != nil {
			t.Fatalf("first: %v", err)
		}
		again, err := svc.BulkMarkWatched(ctx, userID, p)
		if err != nil {
			t.Fatalf("second: %v", err)
		}
		if again.Created != 0 {
			t.Errorf("repeat created %d rows, want 0", again.Created)
		}

		re, err := svc.BulkMarkWatched(ctx, userID, seasonScope(byKey(2), "episode_air", nil, true))
		if err != nil {
			t.Fatalf("rewatch: %v", err)
		}
		if re.Created != s2Aired {
			t.Errorf("rewatch created = %d, want %d", re.Created, s2Aired)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 2*s2Aired {
			t.Errorf("watch rows = %d, want %d — the rewatch must stay inside season 2", n, 2*s2Aired)
		}
	})

	// The season_air pre-check is scoped with the insert: a missing premiere on
	// ANOTHER season must not demand a fallback, and one on the marked season
	// must.
	t.Run("season_air checks only the marked season's premiere", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			_, txErr := tx.Exec(ctx, `UPDATE seasons SET air_date = NULL WHERE season_number = 2`)
			return txErr
		})
		if err != nil {
			t.Fatalf("nulling the premiere: %v", err)
		}

		// Season 1 still dates fine without a fallback.
		if _, err := svc.BulkMarkWatched(ctx, userID, seasonScope(byKey(1), "season_air", nil, false)); err != nil {
			t.Fatalf("season 1 season_air: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_watch_history WHERE watched_at = '2008-01-20T00:00:00Z'`); n != s1Aired {
			t.Errorf("season 1 rows at its premiere = %d, want %d", n, s1Aired)
		}

		// Season 2 without a fallback: refused, and nothing new lands.
		if _, err := svc.BulkMarkWatched(ctx, userID, seasonScope(byKey(2), "season_air", nil, false)); !errors.Is(err, httpx.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != s1Aired {
			t.Fatalf("watch rows after the refusal = %d, want %d", n, s1Aired)
		}

		// With one: season 2's episodes take it.
		if _, err := svc.BulkMarkWatched(ctx, userID, seasonScope(byKey(2), "season_air", strPtr("2010-01-01T20:00:00Z"), false)); err != nil {
			t.Fatalf("season 2 with fallback: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_watch_history WHERE watched_at = '2010-01-01T20:00:00Z'`); n != s2Aired {
			t.Errorf("fallback-dated rows = %d, want %d", n, s2Aired)
		}
	})

	t.Run("an untracked show materialises whole on a season mark", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.BulkMarkWatched(ctx, userID, seasonScope(byKey(2), "", nil, false))
		if err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		if res.Created != s2Aired {
			t.Errorf("created = %d, want %d", res.Created, s2Aired)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes`); n != 5 {
			t.Errorf("episodes = %d, want 5 — marking a season must pull the whole show local", n)
		}
	})

	// A season target_id resolves its show through the row's own show_id, which
	// is NOT NULL: only tracking writes a season, so the link is always local
	// and this path never reaches TMDB.
	t.Run("a season target_id resolves its show without a provider call", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		season1, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 1)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		res, err := svc.BulkMarkWatched(ctx, userID, seasonScope(
			common.TrackParams{TargetType: "season", TargetID: &season1.ID}, "", nil, false))
		if err != nil {
			t.Fatalf("BulkMarkWatched: %v", err)
		}
		if res.Created != s1Aired {
			t.Errorf("created = %d, want %d", res.Created, s1Aired)
		}
	})

	t.Run("specials are refused", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			_, txErr := tx.Exec(ctx, `
INSERT INTO seasons (show_id, show_tmdb_id, season_number, name, show_title, metadata_json, fetched_at)
VALUES (?, 1396, 0, 'Specials', 'Breaking Bad', '{}', '2026-08-19T12:00:00Z')`, show.ID)
			return txErr
		})
		if err != nil {
			t.Fatalf("seeding season 0: %v", err)
		}
		if _, err := svc.BulkMarkWatched(ctx, userID, seasonScope(byKey(0), "", nil, false)); !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("error = %v, want ErrValidation — specials never gate completion", err)
		}
	})

	t.Run("bad season addressing", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := trackedShowService(t)
		season1, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 1)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		tests := []struct {
			name  string
			scope string
			p     common.TrackParams
			want  error
		}{
			{"scope season with a show target", "season",
				common.TrackParams{TargetType: "show", TMDBID: videotest.PtrInt64(1396)}, httpx.ErrValidation},
			{"scope show with a season target", "show", byKey(2), httpx.ErrValidation},
			{"episode field on a season target", "season",
				common.TrackParams{TargetType: "season", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1)},
				httpx.ErrValidation},
			{"season alongside a local id", "season",
				common.TrackParams{TargetType: "season", TargetID: &season1.ID, Season: videotest.PtrInt(1)}, httpx.ErrValidation},
			{"tmdb_id without a season number", "season",
				common.TrackParams{TargetType: "season", TMDBID: videotest.PtrInt64(1396)}, httpx.ErrValidation},
			{"a negative season", "season",
				common.TrackParams{TargetType: "season", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(-1)}, httpx.ErrValidation},
			{"a season the show does not have", "season",
				common.TrackParams{TargetType: "season", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(9)}, httpx.ErrNotFound},
			{"an unknown local season id", "season",
				common.TrackParams{TargetType: "season", TargetID: videotest.PtrInt64(9999)}, httpx.ErrNotFound},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				_, err := svc.BulkMarkWatched(ctx, userID, BulkWatchParams{Scope: tc.scope, TrackParams: tc.p})
				if !errors.Is(err, tc.want) {
					t.Errorf("error = %v, want %v", err, tc.want)
				}
			})
		}
	})
}
