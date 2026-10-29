package details

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
)

func TestSimilarTitles(t *testing.T) {
	t.Parallel()

	t.Run("keeps TMDB's order and drops entries without an id or a title", func(t *testing.T) {
		t.Parallel()
		got := common.SimilarTitles(snapFor(t, videotest.MatrixDetailsBody))
		if len(got) != 2 || got[0].TMDBID != 604 || got[1].TMDBID != 606 {
			t.Fatalf("similar = %+v, want 604 then 606", got)
		}
		if got[0].Title != "The Matrix Reloaded" {
			t.Errorf("title = %q", got[0].Title)
		}
	})

	t.Run("reads the TV shape: name and first_air_date", func(t *testing.T) {
		t.Parallel()
		snap := snapFor(t, `{"recommendations": {"results": [
			{"id": 60059, "name": "Better Call Saul", "first_air_date": "2015-02-08",
			 "poster_path": "/bcs.jpg", "vote_average": 8.7}]}}`)
		got := common.SimilarTitles(snap)
		if len(got) != 1 || got[0].Title != "Better Call Saul" {
			t.Fatalf("similar = %+v, want the show read off name", got)
		}
		if got[0].Year == nil || *got[0].Year != "2015" {
			t.Errorf("year = %v, want 2015 from first_air_date", got[0].Year)
		}
		if got[0].Rating == nil || *got[0].Rating != 8.7 {
			t.Errorf("rating = %v, want 8.7", got[0].Rating)
		}
		if got[0].Poster == nil || *got[0].Poster != "api/v1/video/posters/w185/bcs.jpg" ||
			got[0].PosterFull == nil || *got[0].PosterFull != "api/v1/video/posters/w500/bcs.jpg" {
			t.Errorf("posters = %v / %v, want the proxy variants — a foreign title has no asset directory",
				got[0].Poster, got[0].PosterFull)
		}
	})

	t.Run("a missing date, poster or score renders null", func(t *testing.T) {
		t.Parallel()
		got := common.SimilarTitles(snapFor(t, `{"recommendations": {"results": [
			{"id": 7, "title": "Bare", "release_date": "", "poster_path": "", "vote_average": 0}]}}`))
		if len(got) != 1 {
			t.Fatalf("similar = %+v, want one entry", got)
		}
		if got[0].Year != nil || got[0].Rating != nil || got[0].Poster != nil || got[0].PosterFull != nil {
			t.Errorf("entry = %+v, want every optional field null", got[0])
		}
	})

	t.Run("caps at similarMax", func(t *testing.T) {
		t.Parallel()
		entries := make([]string, 0, common.SimilarMax+5)
		for i := 1; i <= common.SimilarMax+5; i++ {
			entries = append(entries, fmt.Sprintf(`{"id": %d, "title": "T%d"}`, i, i))
		}
		got := common.SimilarTitles(snapFor(t, `{"recommendations": {"results": [`+strings.Join(entries, ",")+`]}}`))
		if len(got) != common.SimilarMax || got[0].TMDBID != 1 || got[common.SimilarMax-1].TMDBID != common.SimilarMax {
			t.Errorf("len = %d (first %d, last %d), want the first %d in order",
				len(got), got[0].TMDBID, got[len(got)-1].TMDBID, common.SimilarMax)
		}
	})

	t.Run("no section and an empty section both give an empty, non-nil list", func(t *testing.T) {
		t.Parallel()
		for _, body := range []string{`{"id": 1}`, `{"recommendations": {"results": []}}`} {
			got := common.SimilarTitles(snapFor(t, body))
			if got == nil || len(got) != 0 {
				t.Errorf("body %s: similar = %#v, want []", body, got)
			}
		}
	})
}

// A snapshot written before `recommendations` was appended must refetch once on
// its next view — the same gate the aggregate cast and the watch providers went
// through — and never again once the refetched body carries the key.
func TestSimilarRefreshGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Every stored show right before this append: aggregate cast and a pruned
	// provider listing present, no recommendations key.
	const legacyShow = `{"id": 1396, "name": "Breaking Bad", "seasons": [],
		"aggregate_credits": {"cast": []}, "watch/providers": {"results": {}}}`
	const freshShow = `{"id": 1396, "name": "Breaking Bad", "seasons": [],
		"aggregate_credits": {"cast": []}, "watch/providers": {"results": {}},
		"recommendations": {"results": [
			{"id": 60059, "name": "Better Call Saul", "first_air_date": "2015-02-08"}]}}`

	for _, byID := range []bool{false, true} {
		byID := byID
		name := "by tmdb_id"
		if byID {
			name = "by local id"
		}
		t.Run("a show snapshot predating recommendations refreshes once, even when fresh, "+name, func(t *testing.T) {
			t.Parallel()
			stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": legacyShow}}
			svc, _ := detailsService(t, stub)

			row, err := svc.MaterialiseShow(ctx, 1396)
			if err != nil {
				t.Fatalf("materialiseShow: %v", err)
			}
			stub.Mu.Lock()
			stub.Bodies["/3/tv/1396"] = freshShow
			stub.Mu.Unlock()
			before := stub.CallCount("/3/tv/1396")

			params := ShowDetailsParams{TMDBID: "1396"}
			if byID {
				params = ShowDetailsParams{ID: fmt.Sprint(row.ID)}
			}
			d, err := svc.ShowDetails(ctx, 0, params)
			if err != nil {
				t.Fatalf("ShowDetails: %v", err)
			}
			if stub.CallCount("/3/tv/1396") != before+1 {
				t.Fatalf("tmdb calls = %d, want %d — the missing section forces one refresh",
					stub.CallCount("/3/tv/1396"), before+1)
			}
			if len(d.Similar) != 1 || d.Similar[0].TMDBID != 60059 {
				t.Fatalf("similar = %+v, want Better Call Saul after the refresh", d.Similar)
			}

			if _, err := svc.ShowDetails(ctx, 0, params); err != nil {
				t.Fatalf("second ShowDetails: %v", err)
			}
			if stub.CallCount("/3/tv/1396") != before+1 {
				t.Errorf("tmdb calls = %d, want still %d", stub.CallCount("/3/tv/1396"), before+1)
			}
		})
	}

	t.Run("when the forced show refresh fails the stale snapshot serves an empty rail", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": legacyShow}}
		svc, _ := detailsService(t, stub)
		if _, err := svc.MaterialiseShow(ctx, 1396); err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		stub.Mu.Lock()
		delete(stub.Bodies, "/3/tv/1396") // an unknown path is the stub's outage
		stub.Mu.Unlock()

		d, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{TMDBID: "1396"})
		if err != nil {
			t.Fatalf("ShowDetails: %v, want the stale snapshot served", err)
		}
		if d.Similar == nil || len(d.Similar) != 0 {
			t.Errorf("similar = %#v, want [] off the legacy snapshot", d.Similar)
		}
	})

	t.Run("a movie snapshot predating recommendations refreshes once, even when fresh", func(t *testing.T) {
		t.Parallel()
		// external_ids, images and providers present: the shape every stored
		// movie had right before this append. The recommendations probe alone
		// must trigger the refresh.
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/movie/603": `{"id": 603, "title": "The Matrix",
				"external_ids": {"imdb_id": "tt0133093"}, "videos": {"results": []},
				"images": {"backdrops": [], "posters": []}, "watch/providers": {"results": {}}}`,
		}}
		svc, _ := detailsService(t, stub)

		if _, err := svc.RefreshMovie(ctx, 603); err != nil {
			t.Fatalf("refreshMovie: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 1 {
			t.Fatalf("tmdb calls after materialising = %d, want 1", stub.CallCount("/3/movie/603"))
		}

		stub.Mu.Lock()
		stub.Bodies["/3/movie/603"] = videotest.MatrixDetailsBody
		stub.Mu.Unlock()

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("second MovieDetails: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 2 {
			t.Errorf("tmdb calls after second view = %d, want 2", stub.CallCount("/3/movie/603"))
		}
		if len(d.Similar) != 2 {
			t.Errorf("similar = %+v, want the refetched rail", d.Similar)
		}

		if _, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"}); err != nil {
			t.Fatalf("third MovieDetails: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 2 {
			t.Errorf("tmdb calls after third view = %d, want still 2", stub.CallCount("/3/movie/603"))
		}
	})
}
