package details

import (
	"context"
	"strconv"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
)

// The season grid's two derived per-user reads — progress and the average of
// the user's episode ratings — on ShowSeason. The copies test's shape
// (service_copies_test.go): the fixture is Breaking Bad with S1 = one aired
// episode and S2 = four, of which S2E2 is undated and so not aired.
func TestShowDetailsSeasonStats(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a tracked show carries per-season progress and the rating average", func(t *testing.T) {
		t.Parallel()
		svc, alice, show := trackedShowService(t)
		bob := videotest.SeedUser(t, svc.DB, "bob")
		episode := func(season, number int) int64 {
			row, err := common.GetEpisodeByShowKey(ctx, svc.DB, show.ID, season, number)
			if err != nil {
				t.Fatalf("getEpisodeByShowKey(%d, %d): %v", season, number, err)
			}
			return row.ID
		}
		s1e1, s2e1, s2e3 := episode(1, 1), episode(2, 1), episode(2, 3)

		// Two watches of one episode: progress counts episodes, not rows (D-009).
		videotest.SeedWatch(t, svc.DB, alice, "episode", s2e1, "2026-08-01T20:00:00Z")
		videotest.SeedWatch(t, svc.DB, alice, "episode", s2e1, "2026-08-02T20:00:00Z")
		videotest.SeedRating(t, svc.DB, alice, "episode", s2e1, 8)
		videotest.SeedRating(t, svc.DB, alice, "episode", s2e3, 6)
		// Another user's state must not leak into Alice's grid.
		videotest.SeedWatch(t, svc.DB, bob, "episode", s1e1, "2026-08-01T20:00:00Z")
		videotest.SeedRating(t, svc.DB, bob, "episode", s1e1, 10)
		// Same id, wrong table: a movie watch or rating carrying an episode's id
		// is not that episode's — the pair identifies the target.
		videotest.SeedWatch(t, svc.DB, alice, "movie", s1e1, "2026-08-01T20:00:00Z")
		videotest.SeedRating(t, svc.DB, alice, "movie", s1e1, 1)

		// Both addressing forms fall through to one gate, so both must carry
		// the stats — the id path is how the page re-reads itself.
		for _, addr := range []ShowDetailsParams{{TMDBID: "1396"}, {ID: strconv.FormatInt(show.ID, 10)}} {
			d, err := svc.ShowDetails(ctx, alice, addr)
			if err != nil {
				t.Fatalf("ShowDetails(%+v): %v", addr, err)
			}
			if len(d.Seasons) != 2 {
				t.Fatalf("seasons = %d, want 2", len(d.Seasons))
			}
			s1, s2 := d.Seasons[0], d.Seasons[1]
			if want := (common.ShowProgressCounts{Watched: 0, AiredTotal: 1, Total: 1}); s1.Progress == nil || *s1.Progress != want {
				t.Errorf("%+v season 1 progress = %+v, want %+v", addr, s1.Progress, want)
			}
			if s1.UserRating != nil {
				t.Errorf("%+v season 1 user_rating = %v, want nil (only Bob rated it)", addr, *s1.UserRating)
			}
			if want := (common.ShowProgressCounts{Watched: 1, AiredTotal: 3, Total: 4}); s2.Progress == nil || *s2.Progress != want {
				t.Errorf("%+v season 2 progress = %+v, want %+v", addr, s2.Progress, want)
			}
			if s2.UserRating == nil || *s2.UserRating != 7 {
				t.Errorf("%+v season 2 user_rating = %v, want 7 (mean of 8 and 6)", addr, s2.UserRating)
			}
			// The show-wide pair: Alice's two episode ratings and nothing else —
			// not Bob's, not the movie row wearing an episode's id.
			if d.UserEpisodeRating == nil || *d.UserEpisodeRating != 7 || d.UserRatedEpisodes != 2 {
				t.Errorf("%+v show episode average = %v over %d rated, want 7 over 2",
					addr, d.UserEpisodeRating, d.UserRatedEpisodes)
			}
		}
	})

	t.Run("an untracked show has neither", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		d, err := svc.ShowDetails(ctx, userID, ShowDetailsParams{TMDBID: "1396"})
		if err != nil {
			t.Fatalf("ShowDetails: %v", err)
		}
		if len(d.Seasons) != 2 {
			t.Fatalf("seasons = %d, want 2", len(d.Seasons))
		}
		if d.UserEpisodeRating != nil || d.UserRatedEpisodes != 0 {
			t.Errorf("show episode average = %v over %d, want null over 0 on an untracked show",
				d.UserEpisodeRating, d.UserRatedEpisodes)
		}
		for _, s := range d.Seasons {
			if s.Progress != nil || s.UserRating != nil {
				t.Errorf("season %d = progress %+v, user_rating %v; want both null on an untracked show",
					s.SeasonNumber, s.Progress, s.UserRating)
			}
		}
	})
}
