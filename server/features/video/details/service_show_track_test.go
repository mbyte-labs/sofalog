package details

import (
	"context"
	"testing"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/provider"
)

func TestMaterialiseShow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("writes the show, every season and every episode", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)

		row, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		if row.ID < 1 || row.Title != "Breaking Bad" {
			t.Fatalf("show row = %+v", row)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM seasons WHERE show_id = ?`, row.ID); n != 2 {
			t.Errorf("seasons = %d, want 2", n)
		}
		// Season 1 has one episode in the fixture, season 2 has four.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes WHERE show_id = ?`, row.ID); n != 5 {
			t.Errorf("episodes = %d, want 5", n)
		}
		// Every season it materialised is linked, which is what roots the
		// asset paths and what D-008's reference check walks.
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM seasons WHERE show_id IS NULL`); n != 0 {
			t.Errorf("unlinked seasons = %d, want 0", n)
		}
	})

	t.Run("is convergent, not duplicative", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)

		first, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("first materialiseShow: %v", err)
		}
		second, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("second materialiseShow: %v", err)
		}
		if first.ID != second.ID {
			t.Errorf("show id changed: %d then %d", first.ID, second.ID)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM shows`); n != 1 {
			t.Errorf("show rows = %d, want 1", n)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes`); n != 5 {
			t.Errorf("episode rows = %d, want 5", n)
		}
	})

	// All-or-nothing: the recompute reads every episode of a show, so a
	// half-written show would report a status that is not reproducible from
	// the log.
	t.Run("a season failing part-way writes nothing", func(t *testing.T) {
		t.Parallel()
		inner := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, inner)
		// Let the show root and season 1 through, then fail season 2.
		svc.TMDB = provider.NewTMDB(provider.TMDBCredentials{APIKeyV3: "test-key"},
			provider.WithHTTPDoer(&videotest.FailAfter{Inner: inner, N: 2}))

		if _, err := svc.MaterialiseShow(ctx, 1396); err == nil {
			t.Fatal("materialiseShow succeeded, want a provider error")
		}
		for _, table := range []string{"shows", "seasons", "episodes"} {
			if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM `+table); n != 0 {
				t.Errorf("%s rows = %d, want 0 — a failed materialise must write nothing", table, n)
			}
		}
	})

	// A season TMDB adds to a tracked show is materialised by whichever page
	// reaches it first — the season view here, RefreshShow when the user opens
	// the show page instead. Either way it arrives linked and with its episodes,
	// or the asset diff and the mark path would never see it.
	t.Run("a season view links itself to the tracked show and writes its episodes", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithOneSeasonBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")
		show, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		// A reference, so the asset sync actually enqueues (D-008).
		videotest.SeedWatchlist(t, svc.DB, userID, "show", show.ID, "2026-08-19T12:00:00Z")

		// TMDB announces season 2; the user opens its page before the show's
		// next stale refresh.
		stub.Mu.Lock()
		stub.Bodies["/3/tv/1396"] = videotest.ShowWithSeasonBody
		stub.Mu.Unlock()

		got, err := svc.SeasonDetails(ctx, userID, SeasonDetailsParams{TMDBID: "1396", Season: "2"})
		if err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM seasons WHERE show_id IS NULL`); n != 0 {
			t.Errorf("unlinked seasons = %d, want 0 — the view left an orphan", n)
		}
		// Season 1's one episode plus season 2's four.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes WHERE show_id = ?`, show.ID); n != 5 {
			t.Errorf("episodes = %d, want 5 — the view did not write season 2's rows", n)
		}
		// The very first render is already markable: local episode ids, not nulls.
		for i, ep := range got.Episodes {
			if ep.ID == nil {
				t.Errorf("episodes[%d].id is null, want the row written by this view", i)
			}
		}
		// And the new season's artwork joined the show's asset diff.
		sources, err := assets.ListJobSources(ctx, svc.DB, "show", show.ID)
		if err != nil {
			t.Fatalf("ListJobSources: %v", err)
		}
		for _, dest := range []string{
			assets.ShowSeasonPoster(show.ID, 2, true),
			assets.ShowEpisodeStill(show.ID, 2, 1, true),
		} {
			if _, ok := sources[dest]; !ok {
				t.Errorf("no asset job for %s — the new season's artwork stayed cache-only", dest)
			}
		}
	})

	t.Run("refreshShow materialises a season the show gained", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithOneSeasonBody}}
		svc, _ := detailsService(t, stub)
		show, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}

		stub.Mu.Lock()
		stub.Bodies["/3/tv/1396"] = videotest.ShowWithSeasonBody
		stub.Mu.Unlock()

		if _, err := svc.RefreshShow(ctx, 1396); err != nil {
			t.Fatalf("refreshShow: %v", err)
		}
		season, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 2)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		if season.ShowID == nil || *season.ShowID != show.ID {
			t.Errorf("season.ShowID = %v, want %d", season.ShowID, show.ID)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes WHERE show_id = ?`, show.ID); n != 5 {
			t.Errorf("episodes = %d, want 5 — the refresh did not write season 2's rows", n)
		}
	})

	// D-015 with no season exception left: a view of an untracked show stores
	// nothing at all, seasons included.
	t.Run("a season view of an untracked show stores nothing", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)

		got, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{TMDBID: "1396", Season: "2"})
		if err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		for _, table := range []string{"shows", "seasons", "episodes", "asset_jobs"} {
			if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM `+table); n != 0 {
				t.Errorf("%s rows = %d, want 0 — an untracked view must store nothing", table, n)
			}
		}
		// The page still renders in full; only the ids are absent.
		if got.ID != nil {
			t.Errorf("id = %d, want null", *got.ID)
		}
		if got.Name != "Season 2" || len(got.Episodes) != 4 {
			t.Errorf("live projection = %q with %d episodes, want Season 2 with 4", got.Name, len(got.Episodes))
		}
		for i, ep := range got.Episodes {
			if ep.ID != nil {
				t.Errorf("episodes[%d].id = %d, want null", i, *ep.ID)
			}
		}
	})

	t.Run("resolveTrackedShow reuses an existing row without refetching", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)

		if _, err := svc.ResolveTrackedShow(ctx, 1396); err != nil {
			t.Fatalf("first resolveTrackedShow: %v", err)
		}
		before := stub.CallCount("/3/tv/1396")
		if _, err := svc.ResolveTrackedShow(ctx, 1396); err != nil {
			t.Fatalf("second resolveTrackedShow: %v", err)
		}
		if after := stub.CallCount("/3/tv/1396"); after != before {
			t.Errorf("calls went %d → %d; tracking an already-tracked show must not refetch", before, after)
		}
	})
}
