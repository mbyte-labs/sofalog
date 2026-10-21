package upnext

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/features/video/watches"
	"mbyte.ro/sofalog/httpx"
)

// testService carries no DB at all: parameter validation must reject before
// anything could reach the store, and a nil pointer proves it does.
func testService() *Service {
	return &Service{Core: &common.Core{Now: videotest.FixedClock}}
}

func upNextService(t *testing.T) *Service {
	t.Helper()
	return &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
}

func showNames(entries []UpNextEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Show.Name
	}
	return names
}

func upNext(t *testing.T, svc *Service, userID int64, p UpNextParams) []UpNextEntry {
	t.Helper()
	entries, err := svc.UpNext(context.Background(), userID, p)
	if err != nil {
		t.Fatalf("UpNext(%+v) error = %v", p, err)
	}
	return entries
}

// oneEntry asserts the list holds exactly one entry and returns it.
func oneEntry(t *testing.T, entries []UpNextEntry) UpNextEntry {
	t.Helper()
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1 (%v)", len(entries), showNames(entries))
	}
	return entries[0]
}

func wantEpisode(t *testing.T, e UpNextEntry, season, number int) {
	t.Helper()
	if e.Episode.Season != season || e.Episode.Number != number {
		t.Fatalf("next = S%dE%d, want S%dE%d", e.Episode.Season, e.Episode.Number, season, number)
	}
}

func TestUpNextPick(t *testing.T) {
	t.Parallel()
	aired := videotest.Ptr("2026-01-01")

	t.Run("watched episode 2 offers episode 3, never episode 1", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		e2 := videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 3, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e2, "2026-08-01T20:00:00Z")

		e := oneEntry(t, upNext(t, svc, user, UpNextParams{}))
		wantEpisode(t, e, 1, 3)
		if !e.Episode.Aired {
			t.Errorf("aired = false, want true for a past air date")
		}
	})

	t.Run("gap in the log skips to after the highest watched", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		var ids []int64
		for n := 1; n <= 6; n++ {
			ids = append(ids, videotest.SeedEpisode(t, svc.DB, show, 1, n, aired))
		}
		videotest.SeedWatch(t, svc.DB, user, "episode", ids[1], "2026-08-01T20:00:00Z") // E2
		videotest.SeedWatch(t, svc.DB, user, "episode", ids[4], "2026-08-02T20:00:00Z") // E5

		wantEpisode(t, oneEntry(t, upNext(t, svc, user, UpNextParams{})), 1, 6)
	})

	t.Run("season finale watched rolls over to the next season", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		e12 := videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedEpisode(t, svc.DB, show, 2, 1, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e12, "2026-08-01T20:00:00Z")

		wantEpisode(t, oneEntry(t, upNext(t, svc, user, UpNextParams{})), 2, 1)
	})

	t.Run("finished show is absent, never wrapping back to episode 1", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired) // unwatched, and must stay unoffered
		last := videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", last, "2026-08-01T20:00:00Z")

		if entries := upNext(t, svc, user, UpNextParams{}); len(entries) != 0 {
			t.Fatalf("finished show listed: %v", showNames(entries))
		}
	})

	t.Run("never-started show is absent even when watchlisted", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedWatchlist(t, svc.DB, user, "show", show, "2026-08-01T20:00:00Z")

		if entries := upNext(t, svc, user, UpNextParams{}); len(entries) != 0 {
			t.Fatalf("never-started show listed: %v", showNames(entries))
		}
	})

	t.Run("dropped show is absent", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		videotest.SeedShowStatus(t, svc.DB, user, show, "dropped")

		if entries := upNext(t, svc, user, UpNextParams{}); len(entries) != 0 {
			t.Fatalf("dropped show listed: %v", showNames(entries))
		}
	})

	// The whole point of the drop endpoint, end to end: the exclusion above is
	// reached through the real write, and releasing it puts the card back.
	t.Run("dropping and un-dropping removes and restores the card", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		videotest.Recompute(t, svc.DB, user, show)
		wantEpisode(t, oneEntry(t, upNext(t, svc, user, UpNextParams{})), 1, 2)

		if _, err := watches.NewService(svc.Core).DropShow(context.Background(), user, videotest.ShowPath(show)); err != nil {
			t.Fatalf("DropShow: %v", err)
		}
		if entries := upNext(t, svc, user, UpNextParams{}); len(entries) != 0 {
			t.Fatalf("dropped show listed: %v", showNames(entries))
		}

		if _, err := watches.NewService(svc.Core).UndropShow(context.Background(), user, videotest.ShowPath(show)); err != nil {
			t.Fatalf("UndropShow: %v", err)
		}
		wantEpisode(t, oneEntry(t, upNext(t, svc, user, UpNextParams{})), 1, 2)
	})

	t.Run("specials never anchor the pick nor become it", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		s0 := videotest.SeedEpisode(t, svc.DB, show, 0, 1, aired)
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 0, 2, aired) // unwatched special: never a candidate
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)

		// Only a special watched: the show is started, and the vacuous anchor
		// degenerates to the first regular episode.
		videotest.SeedWatch(t, svc.DB, user, "episode", s0, "2026-08-01T20:00:00Z")
		wantEpisode(t, oneEntry(t, upNext(t, svc, user, UpNextParams{})), 1, 1)

		// A regular watch anchors past the unwatched special.
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-02T20:00:00Z")
		wantEpisode(t, oneEntry(t, upNext(t, svc, user, UpNextParams{})), 1, 2)
	})

	t.Run("users are isolated: watches, drops and picks", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		alice := videotest.SeedUser(t, svc.DB, "alice@example.com")
		bob := videotest.SeedUser(t, svc.DB, "bob@example.com")
		carol := videotest.SeedUser(t, svc.DB, "carol@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		e2 := videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 3, aired)

		videotest.SeedWatch(t, svc.DB, alice, "episode", e2, "2026-08-01T20:00:00Z")
		videotest.SeedWatch(t, svc.DB, bob, "episode", e1, "2026-08-01T20:00:00Z")
		videotest.SeedShowStatus(t, svc.DB, bob, show, "dropped")

		wantEpisode(t, oneEntry(t, upNext(t, svc, alice, UpNextParams{})), 1, 3)
		if entries := upNext(t, svc, bob, UpNextParams{}); len(entries) != 0 {
			t.Fatalf("bob dropped the show, got %v", showNames(entries))
		}
		if entries := upNext(t, svc, carol, UpNextParams{}); len(entries) != 0 {
			t.Fatalf("carol never watched anything, got %v", showNames(entries))
		}
	})

	t.Run("a movie watch sharing an episode id neither starts nor advances a show", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		// Movie ids and episode ids come from different tables (D-013): a
		// movie watch carrying e1's number must be invisible here.
		videotest.SeedWatch(t, svc.DB, user, "movie", e1, "2026-08-01T20:00:00Z")

		if entries := upNext(t, svc, user, UpNextParams{}); len(entries) != 0 {
			t.Fatalf("movie watch started a show: %v", showNames(entries))
		}
	})
}

func TestUpNextAiredFlagAndFilters(t *testing.T) {
	t.Parallel()
	aired := videotest.Ptr("2026-01-01")
	future := videotest.Ptr("2027-01-01")

	// Three started shows whose next episode differs only in air date.
	newFixture := func(t *testing.T) (*Service, int64) {
		t.Helper()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		for i, next := range []*string{aired, future, nil} {
			show := videotest.SeedShow(t, svc.DB, int64(100+i), fmt.Sprintf("Show %d", i))
			e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
			videotest.SeedEpisode(t, svc.DB, show, 1, 2, next)
			videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		}
		return svc, user
	}

	t.Run("unfiltered keeps unaired and undated picks, flagged", func(t *testing.T) {
		t.Parallel()
		svc, user := newFixture(t)
		entries := upNext(t, svc, user, UpNextParams{OrderBy: "show_name", Order: "asc"})
		if len(entries) != 3 {
			t.Fatalf("len = %d, want 3 (%v)", len(entries), showNames(entries))
		}
		for i, wantAired := range []bool{true, false, false} {
			if entries[i].Episode.Aired != wantAired {
				t.Errorf("%s: aired = %v, want %v", entries[i].Show.Name, entries[i].Episode.Aired, wantAired)
			}
		}
	})

	t.Run("only_with_release_date drops the undated pick", func(t *testing.T) {
		t.Parallel()
		svc, user := newFixture(t)
		entries := upNext(t, svc, user, UpNextParams{OrderBy: "show_name", Order: "asc", OnlyWithReleaseDate: "true"})
		if len(entries) != 2 || entries[0].Show.Name != "Show 0" || entries[1].Show.Name != "Show 1" {
			t.Fatalf("got %v, want [Show 0, Show 1]", showNames(entries))
		}
	})

	t.Run("only_released drops the undated and future picks", func(t *testing.T) {
		t.Parallel()
		svc, user := newFixture(t)
		entries := upNext(t, svc, user, UpNextParams{OnlyReleased: "1"})
		e := oneEntry(t, entries)
		if e.Show.Name != "Show 0" {
			t.Fatalf("got %q, want Show 0", e.Show.Name)
		}
	})

	t.Run("an unaired pick never falls back to a later aired episode", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, future)
		videotest.SeedEpisode(t, svc.DB, show, 1, 3, aired) // aired out of order
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")

		// The pick is E2 regardless of airedness…
		wantEpisode(t, oneEntry(t, upNext(t, svc, user, UpNextParams{})), 1, 2)
		// …so filtering to released yields nothing, not E3.
		if entries := upNext(t, svc, user, UpNextParams{OnlyReleased: "true"}); len(entries) != 0 {
			t.Fatalf("fell back past the unaired pick: %v", showNames(entries))
		}
	})
}

func TestUpNextEntryFields(t *testing.T) {
	t.Parallel()
	aired := videotest.Ptr("2026-01-01")

	t.Run("last watched, progress, rating, marker and season poster", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 3, videotest.Ptr("2027-01-01"))
		seasonID := videotest.SeedSeasonSnapshot(t, svc.DB, show, 1396, 1, videotest.Ptr("/s1.jpg"),
			`{"episodes": [{"episode_number": 2, "vote_average": 8.4, "episode_type": "mid_season"}]}`)
		// A second show's season, so a season_id matched on number alone
		// rather than on the owning show would pick the wrong row.
		other := videotest.SeedShow(t, svc.DB, 1400, "Other Show")
		videotest.SeedSeasonSnapshot(t, svc.DB, other, 1400, 1, nil, `{"episodes": []}`)

		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		// A rewatch is a later row for the same episode: MAX must take it.
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-05T22:30:00Z")

		e := oneEntry(t, upNext(t, svc, user, UpNextParams{}))
		wantEpisode(t, e, 1, 2)
		if e.LastWatchedAt == nil || *e.LastWatchedAt != "2026-08-05T22:30:00Z" {
			t.Errorf("last_watched_at = %v, want the rewatch instant", e.LastWatchedAt)
		}
		if e.Progress != (UpNextProgress{Watched: 1, AiredTotal: 2, Total: 3}) {
			t.Errorf("progress = %+v, want 1/2/3", e.Progress)
		}
		if e.Episode.Rating == nil || *e.Episode.Rating != 8.4 {
			t.Errorf("rating = %v, want 8.4 from the season snapshot", e.Episode.Rating)
		}
		if e.Episode.Marker == nil || *e.Episode.Marker != "mid_season_finale" {
			t.Errorf("marker = %v, want mid_season_finale", e.Episode.Marker)
		}
		if e.Poster == nil || *e.Poster != "api/v1/video/posters/w185/s1.jpg" {
			t.Errorf("poster = %v, want the season poster proxy", e.Poster)
		}
		// The handle the card links the episode details page by; it must be
		// THIS show's season 1, not the other show's.
		if e.Episode.SeasonID == nil || *e.Episode.SeasonID != seasonID {
			t.Errorf("season_id = %v, want %d", e.Episode.SeasonID, seasonID)
		}
	})

	t.Run("season_id is null when the season row is absent", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")

		// Unreachable in practice — an episode is written alongside its season
		// — but the entry must still project, unlinked, rather than failing.
		e := oneEntry(t, upNext(t, svc, user, UpNextParams{}))
		wantEpisode(t, e, 1, 2)
		if e.Episode.SeasonID != nil {
			t.Errorf("season_id = %v, want null", *e.Episode.SeasonID)
		}
	})

	t.Run("poster falls back to the show's, then to null", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")

		withPoster := videotest.SeedShowWithPoster(t, svc.DB, 100, "Alpha", videotest.Ptr("/show.jpg"))
		e1 := videotest.SeedEpisode(t, svc.DB, withPoster, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, withPoster, 1, 2, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")

		bare := videotest.SeedShow(t, svc.DB, 101, "Beta")
		b1 := videotest.SeedEpisode(t, svc.DB, bare, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, bare, 1, 2, aired)
		videotest.SeedWatch(t, svc.DB, user, "episode", b1, "2026-08-01T20:00:00Z")

		entries := upNext(t, svc, user, UpNextParams{OrderBy: "show_name", Order: "asc"})
		if len(entries) != 2 {
			t.Fatalf("len = %d, want 2", len(entries))
		}
		if entries[0].Poster == nil || *entries[0].Poster != "api/v1/video/posters/w185/show.jpg" {
			t.Errorf("poster = %v, want the show poster proxy", entries[0].Poster)
		}
		if entries[1].Poster != nil {
			t.Errorf("poster = %v, want null with no artwork anywhere", *entries[1].Poster)
		}
	})

	t.Run("a garbage season snapshot renders without rating or marker", func(t *testing.T) {
		t.Parallel()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
		e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
		videotest.SeedEpisode(t, svc.DB, show, 1, 2, aired)
		videotest.SeedSeasonSnapshot(t, svc.DB, show, 100, 1, nil, `not json at all`)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")

		e := oneEntry(t, upNext(t, svc, user, UpNextParams{}))
		if e.Episode.Rating != nil || e.Episode.Marker != nil {
			t.Errorf("rating = %v marker = %v, want both null", e.Episode.Rating, e.Episode.Marker)
		}
	})
}

func TestUpNextOrdering(t *testing.T) {
	t.Parallel()
	aired := videotest.Ptr("2026-01-01")

	// Three started shows built to disagree on every key:
	//   name asc:          Aster, Brine, Cobalt
	//   last watched asc:  Cobalt, then Aster and Brine tied on one instant
	//   next air date asc: Brine (2026-02-01), Aster (2026-03-01), Cobalt (null)
	newFixture := func(t *testing.T) (*Service, int64) {
		t.Helper()
		svc := upNextService(t)
		user := videotest.SeedUser(t, svc.DB, "a@example.com")
		seed := func(tmdbID int64, name string, nextAir *string, watchedAt string) {
			show := videotest.SeedShow(t, svc.DB, tmdbID, name)
			e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, aired)
			videotest.SeedEpisode(t, svc.DB, show, 1, 2, nextAir)
			videotest.SeedWatch(t, svc.DB, user, "episode", e1, watchedAt)
		}
		seed(100, "Aster", videotest.Ptr("2026-03-01"), "2026-08-02T20:00:00Z")
		seed(101, "Brine", videotest.Ptr("2026-02-01"), "2026-08-02T20:00:00Z") // ties with Aster
		seed(102, "Cobalt", nil, "2026-08-01T20:00:00Z")
		return svc, user
	}

	tests := []struct {
		name   string
		params UpNextParams
		want   []string
	}{
		// The tied pair falls back to the name tie-break, following the
		// direction like the watchlist's ordering does.
		{"last_watched desc (default)", UpNextParams{}, []string{"Brine", "Aster", "Cobalt"}},
		{"last_watched asc", UpNextParams{OrderBy: "last_watched", Order: "asc"}, []string{"Cobalt", "Aster", "Brine"}},
		{"show_name asc", UpNextParams{OrderBy: "show_name", Order: "asc"}, []string{"Aster", "Brine", "Cobalt"}},
		{"show_name desc", UpNextParams{OrderBy: "show_name", Order: "desc"}, []string{"Cobalt", "Brine", "Aster"}},
		// An undated pick sorts last in BOTH directions.
		{"air_date asc, undated last", UpNextParams{OrderBy: "air_date", Order: "asc"}, []string{"Brine", "Aster", "Cobalt"}},
		{"air_date desc keeps undated last", UpNextParams{OrderBy: "air_date", Order: "desc"}, []string{"Aster", "Brine", "Cobalt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, user := newFixture(t)
			entries := upNext(t, svc, user, tt.params)
			got := showNames(entries)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestUpNextInvalidParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		params UpNextParams
	}{
		{"bad order_by", UpNextParams{OrderBy: "banana"}},
		{"bad order", UpNextParams{Order: "sideways"}},
		{"bad only_with_release_date", UpNextParams{OnlyWithReleaseDate: "maybe"}},
		{"bad only_released", UpNextParams{OnlyReleased: "maybe"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := testService().UpNext(context.Background(), 1, tt.params)
			if !errors.Is(err, httpx.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

// D-038: the aired flag ends at midnight in the USER's zone. At 22:30 UTC on
// the 18th, an episode dated the 19th has aired in Bucharest and not in New
// York — the same row, two honest answers.
func TestUpNextAiredFollowsUserZone(t *testing.T) {
	t.Parallel()
	svc := &Service{Core: &common.Core{Now: videotest.LateEveningClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
	show := videotest.SeedShow(t, svc.DB, 100, "Alpha")
	e1 := videotest.SeedEpisode(t, svc.DB, show, 1, 1, videotest.Ptr("2026-08-01"))
	videotest.SeedEpisode(t, svc.DB, show, 1, 2, videotest.Ptr("2026-08-19"))

	for _, tc := range []struct {
		zone      string
		wantAired bool
	}{{"Europe/Bucharest", true}, {"America/New_York", false}, {"", false}} {
		user := videotest.SeedUser(t, svc.DB, "u"+tc.zone+"@example.com")
		videotest.SetUserTimezone(t, svc.DB, user, tc.zone)
		videotest.SeedWatch(t, svc.DB, user, "episode", e1, "2026-08-01T20:00:00Z")
		e := oneEntry(t, upNext(t, svc, user, UpNextParams{}))
		wantEpisode(t, e, 1, 2)
		if e.Episode.Aired != tc.wantAired {
			t.Errorf("zone %q: aired = %v, want %v", tc.zone, e.Episode.Aired, tc.wantAired)
		}
		released := upNext(t, svc, user, UpNextParams{OnlyReleased: "true"})
		if got := len(released) == 1; got != tc.wantAired {
			t.Errorf("zone %q: only_released kept it = %v, want %v", tc.zone, got, tc.wantAired)
		}
	}
}
