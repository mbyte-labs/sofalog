package jellyfin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/copies"
	"mbyte.ro/sofalog/features/video/details"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// jellyfinService is a details service with a configured Jellyfin client on
// top, the scan runner wired for a manual run and no loop started.
func jellyfinService(t *testing.T, fake *videotest.JellyfinFake) *Service {
	t.Helper()
	svc, _ := detailsService(t, &videotest.ProviderStub{})
	svc.Jellyfin = provider.NewJellyfin("http://jf.test", "k", provider.WithJellyfinHTTPDoer(fake))
	svc.jellyfinScan = newJellyfinRunner(0)
	return svc
}

// seedJellyfinCatalog gives the scan something to match: two movies and one
// show with Specials, season 1 and season 2. Returns the ids the assertions
// need.
type jellyfinCatalog struct {
	matrixID, aliensID int64
	showID             int64
	seasonIDs          map[int]int64 // by season number
}

func seedJellyfinCatalog(t *testing.T, svc *Service) jellyfinCatalog {
	t.Helper()
	c := jellyfinCatalog{seasonIDs: map[int]int64{}}
	c.matrixID = videotest.SeedHistoryMovie(t, svc.DB).ID
	c.aliensID = videotest.SeedMovie(t, svc.DB, 679, "Aliens", videotest.Ptr("1986-07-18"), nil, `{"external_ids": {}, "images": {}}`).ID
	c.showID = videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
	for _, n := range []int{0, 1, 2} {
		c.seasonIDs[n] = videotest.SeedSeasonSnapshot(t, svc.DB, c.showID, 1396, n, nil, `{"episodes": []}`)
	}
	return c
}

func jellyfinRows(t *testing.T, svc *Service) []string {
	t.Helper()
	rows, err := svc.DB.Read(context.Background(),
		`SELECT target_type, target_id, jellyfin_id FROM jellyfin_library ORDER BY target_type, target_id`)
	if err != nil {
		t.Fatalf("reading jellyfin_library: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var tt, jid string
		var id int64
		if err := rows.Scan(&tt, &id, &jid); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, fmt.Sprintf("%s:%d:%s", tt, id, jid))
	}
	return out
}

func TestJellyfinScan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("matches movies by tmdb id and seasons by any episode, replacing the table", func(t *testing.T) {
		t.Parallel()
		fake := &videotest.JellyfinFake{Items: map[string]string{
			"Movie": `[{"Id":"jm1","ProviderIds":{"Tmdb":"603"}},
			          {"Id":"jm2","ProviderIds":{"Tmdb":"999999"}}]`,
			"Series": `[{"Id":"js1","ProviderIds":{"Tmdb":"1396"}},
			           {"Id":"js2","ProviderIds":{"Tmdb":"777"}}]`,
			"Episode": `[{"Id":"e1","SeriesId":"js1","ParentIndexNumber":1,"IndexNumber":1,"LocationType":"FileSystem"},
			            {"Id":"e1b","SeriesId":"js1","ParentIndexNumber":1,"IndexNumber":1,"LocationType":"FileSystem"},
			            {"Id":"e2","SeriesId":"js1","ParentIndexNumber":1,"IndexNumber":2,"LocationType":"FileSystem"},
			            {"Id":"e3","SeriesId":"js1","ParentIndexNumber":2,"IndexNumber":1,"LocationType":"Virtual"},
			            {"Id":"e4","SeriesId":"js1","ParentIndexNumber":0,"IndexNumber":1,"LocationType":"FileSystem"},
			            {"Id":"e5","SeriesId":"js2","ParentIndexNumber":1,"IndexNumber":1,"LocationType":"FileSystem"}]`,
		}}
		svc := jellyfinService(t, fake)
		cat := seedJellyfinCatalog(t, svc)

		rep := svc.runJellyfinScan(ctx)
		if rep.Error != nil {
			t.Fatalf("scan error = %q", *rep.Error)
		}
		if rep.JellyfinMovies != 2 || rep.JellyfinSeries != 2 || rep.JellyfinEpisodes != 5 {
			t.Errorf("counts = %+v", rep)
		}
		if rep.MatchedMovies != 1 || rep.MatchedSeasons != 2 {
			t.Errorf("matched = %d movies, %d seasons; want 1, 2", rep.MatchedMovies, rep.MatchedSeasons)
		}
		want := []string{
			fmt.Sprintf("movie:%d:jm1", cat.matrixID),
			fmt.Sprintf("season:%d:js1", cat.seasonIDs[0]),
			fmt.Sprintf("season:%d:js1", cat.seasonIDs[1]),
		}
		got := jellyfinRows(t, svc)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("rows = %v, want %v", got, want)
		}
		if st := svc.JellyfinStatus(); st.ServerName == nil || *st.ServerName != "Living room" || st.Last == nil || st.Scanning {
			t.Errorf("status = %+v", st)
		}

		// The projection: the matrix and season 1 carry jellyfin; a manual
		// jellyfin row on Aliens is ignored while the scan owns the kind, a
		// manual dvd row survives beside the scan's answer.
		userID := videotest.SeedUser(t, svc.DB, "alice")
		videotest.SeedCopy(t, svc.DB, userID, "movie", cat.aliensID, "jellyfin")
		videotest.SeedCopy(t, svc.DB, userID, "movie", cat.matrixID, "dvd")
		videotest.SeedCopy(t, svc.DB, userID, "season", cat.seasonIDs[2], "bluray")

		if kinds, n, _ := svc.CopiesFor(ctx, userID, "movie", cat.matrixID); !slices.Equal(kinds, []string{"dvd", "jellyfin"}) || n != nil {
			t.Errorf("matrix copies = %v (episodes %v), want [dvd jellyfin] and no count", kinds, n)
		}
		if kinds, _, _ := svc.CopiesFor(ctx, userID, "movie", cat.aliensID); len(kinds) != 0 {
			t.Errorf("aliens copies = %v, want [] — a hand-set jellyfin row is not the scan's answer", kinds)
		}
		// Season 1 has two distinct files, season 0 one; the count is what the
		// page compares with the aired episodes to colour the tile.
		if kinds, n, _ := svc.CopiesFor(ctx, userID, "season", cat.seasonIDs[1]); !slices.Equal(kinds, []string{"jellyfin"}) || n == nil || *n != 2 {
			t.Errorf("season 1 copies = %v, episodes = %v; want [jellyfin], 2", kinds, n)
		}
		if _, n, _ := svc.CopiesFor(ctx, userID, "season", cat.seasonIDs[2]); n != nil {
			t.Errorf("season 2 episodes = %v, want nil — not on the server", n)
		}
		seasons, counts, err := svc.SeasonCopiesFor(ctx, userID, 1396)
		if err != nil {
			t.Fatalf("seasonCopiesFor: %v", err)
		}
		if !slices.Equal(seasons[0], []string{"jellyfin"}) || !slices.Equal(seasons[1], []string{"jellyfin"}) ||
			!slices.Equal(seasons[2], []string{"bluray"}) {
			t.Errorf("season copies = %v", seasons)
		}
		if counts[0] != 1 || counts[1] != 2 || len(counts) != 2 {
			t.Errorf("season episode counts = %v, want {0:1 1:2}", counts)
		}

		d, err := details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{ID: fmt.Sprint(cat.matrixID)})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if !d.JellyfinSynced || !slices.Equal(d.Copies, []string{"dvd", "jellyfin"}) {
			t.Errorf("details synced = %v, copies = %v", d.JellyfinSynced, d.Copies)
		}

		// The next scan no longer sees the movie: its row goes.
		fake.Mu.Lock()
		fake.Items["Movie"] = `[]`
		fake.Mu.Unlock()
		if rep := svc.runJellyfinScan(ctx); rep.Error != nil || rep.MatchedMovies != 0 {
			t.Fatalf("second scan = %+v", rep)
		}
		if kinds, _, _ := svc.CopiesFor(ctx, userID, "movie", cat.matrixID); !slices.Equal(kinds, []string{"dvd"}) {
			t.Errorf("matrix copies after removal = %v, want [dvd]", kinds)
		}
	})

	t.Run("a failing server leaves the previous answer in place", func(t *testing.T) {
		t.Parallel()
		fake := &videotest.JellyfinFake{Items: map[string]string{"Movie": `[{"Id":"jm1","ProviderIds":{"Tmdb":"603"}}]`}}
		svc := jellyfinService(t, fake)
		cat := seedJellyfinCatalog(t, svc)
		if rep := svc.runJellyfinScan(ctx); rep.Error != nil {
			t.Fatalf("first scan: %q", *rep.Error)
		}

		fake.Mu.Lock()
		fake.Fail = errors.New("dial tcp: connection refused")
		fake.Mu.Unlock()
		rep := svc.runJellyfinScan(ctx)
		if rep.Error == nil || *rep.Error != jellyfinErrUnavailable {
			t.Fatalf("error = %v, want %s", rep.Error, jellyfinErrUnavailable)
		}
		if got := jellyfinRows(t, svc); !slices.Equal(got, []string{fmt.Sprintf("movie:%d:jm1", cat.matrixID)}) {
			t.Errorf("rows after a failed scan = %v — must be untouched", got)
		}

		fake.Mu.Lock()
		fake.Fail = nil
		fake.Status = 401 // the key refused
		fake.Mu.Unlock()
		if rep := svc.runJellyfinScan(ctx); rep.Error == nil || *rep.Error != jellyfinErrAuth {
			t.Errorf("error = %v, want %s", rep.Error, jellyfinErrAuth)
		}
		if st := svc.JellyfinStatus(); st.Last == nil || st.Last.Error == nil {
			t.Errorf("status keeps the failed report: %+v", st)
		}
	})

	t.Run("the jellyfin kind is refused by the copy routes while synced, and only then", func(t *testing.T) {
		t.Parallel()
		svc := jellyfinService(t, &videotest.JellyfinFake{})
		cat := seedJellyfinCatalog(t, svc)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		_, err := copies.NewService(svc.Core).CopyAdd(ctx, userID, copies.CopyParams{
			TrackParams: common.TrackParams{TargetType: "movie", TargetID: &cat.matrixID}, Kind: "jellyfin"})
		if !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("CopyAdd jellyfin err = %v, want ErrValidation", err)
		}
		if err := copies.NewService(svc.Core).CopyRemove(ctx, userID, "movie", fmt.Sprint(cat.matrixID), "jellyfin"); !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("CopyRemove jellyfin err = %v, want ErrValidation", err)
		}
		if _, err := copies.NewService(svc.Core).CopyAdd(ctx, userID, copies.CopyParams{
			TrackParams: common.TrackParams{TargetType: "movie", TargetID: &cat.matrixID}, Kind: "dvd"}); err != nil {
			t.Errorf("CopyAdd dvd: %v", err)
		}

		// Unconfigured: the hand-set label works exactly as before.
		svc.Jellyfin = provider.NewJellyfin("", "")
		if _, err := copies.NewService(svc.Core).CopyAdd(ctx, userID, copies.CopyParams{
			TrackParams: common.TrackParams{TargetType: "movie", TargetID: &cat.matrixID}, Kind: "jellyfin"}); err != nil {
			t.Errorf("unsynced CopyAdd jellyfin: %v", err)
		}
		d, err := details.NewService(svc.Core).MovieDetails(ctx, userID, details.MovieDetailsParams{ID: fmt.Sprint(cat.matrixID)})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.JellyfinSynced || !slices.Equal(d.Copies, []string{"dvd", "jellyfin"}) {
			t.Errorf("unsynced details synced = %v, copies = %v", d.JellyfinSynced, d.Copies)
		}
		if st := svc.JellyfinStatus(); st.Configured {
			t.Errorf("status configured = true with no server")
		}
		if err := svc.StartJellyfinScan(); !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("StartJellyfinScan unconfigured err = %v, want ErrNotFound", err)
		}
	})

	t.Run("start is a 409 while running and otherwise kicks the loop", func(t *testing.T) {
		t.Parallel()
		svc := jellyfinService(t, &videotest.JellyfinFake{})
		svc.jellyfinScan.mu.Lock()
		svc.jellyfinScan.running = true
		svc.jellyfinScan.mu.Unlock()
		if err := svc.StartJellyfinScan(); !errors.Is(err, httpx.ErrConflict) {
			t.Errorf("err = %v, want ErrConflict", err)
		}
		svc.jellyfinScan.mu.Lock()
		svc.jellyfinScan.running = false
		svc.jellyfinScan.mu.Unlock()
		if err := svc.StartJellyfinScan(); err != nil {
			t.Fatalf("StartJellyfinScan: %v", err)
		}
		select {
		case <-svc.jellyfinScan.kick:
		default:
			t.Error("no kick queued")
		}
	})

	t.Run("the loop runs on a kick and stops with the context", func(t *testing.T) {
		t.Parallel()
		fake := &videotest.JellyfinFake{Items: map[string]string{"Movie": `[{"Id":"jm1","ProviderIds":{"Tmdb":"603"}}]`}}
		svc := jellyfinService(t, fake)
		cat := seedJellyfinCatalog(t, svc)
		svc.jellyfinScan.bootDelay = time.Hour // only the kick may run it

		loopCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); svc.jellyfinLoop(loopCtx) }()
		if err := svc.StartJellyfinScan(); err != nil {
			t.Fatalf("StartJellyfinScan: %v", err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if st := svc.JellyfinStatus(); st.Last != nil && !st.Scanning {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("the kicked scan never finished")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if got := jellyfinRows(t, svc); !slices.Equal(got, []string{fmt.Sprintf("movie:%d:jm1", cat.matrixID)}) {
			t.Errorf("rows = %v", got)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("loop did not stop on cancel")
		}
	})
}

func TestMergeJellyfin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kinds   []string
		present bool
		want    []string
	}{
		{nil, false, []string{}},
		{nil, true, []string{"jellyfin"}},
		{[]string{"dvd", "jellyfin"}, false, []string{"dvd"}},
		{[]string{"dvd", "uhd"}, true, []string{"dvd", "jellyfin", "uhd"}},
		{[]string{"jellyfin"}, true, []string{"jellyfin"}},
	}
	for _, tt := range tests {
		if got := common.MergeJellyfin(tt.kinds, tt.present); !slices.Equal(got, tt.want) {
			t.Errorf("mergeJellyfin(%v, %v) = %v, want %v", tt.kinds, tt.present, got, tt.want)
		}
	}
}
