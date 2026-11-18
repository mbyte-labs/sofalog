package common_test

import (
	"encoding/json"
	"os"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

// The shared rule fixtures (SPEC D-040): the Android app runs these same cases
// against its Kotlin port of RecomputeShowStatus, so the two cannot drift.
func TestRecomputeShowStatusFixtures(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../fixtures/video-rules/show-status.json")
	if err != nil {
		t.Fatalf("reading fixtures: %v", err)
	}
	var file struct {
		Today string `json:"today"`
		Cases []struct {
			Name     string  `json:"name"`
			Current  *string `json:"current"`
			Want     *string `json:"want"`
			Episodes []struct {
				Season  int     `json:"season"`
				Number  int     `json:"number"`
				AirDate *string `json:"air_date"`
				Watched bool    `json:"watched"`
			} `json:"episodes"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decoding fixtures: %v", err)
	}
	if file.Today != videotest.StatusToday {
		t.Fatalf("fixture today = %q, the recompute helper uses %q", file.Today, videotest.StatusToday)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no fixture cases")
	}
	for _, c := range file.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			database := videotest.DB(t)
			user := videotest.SeedUser(t, database, "a@example.com")
			show := videotest.SeedShow(t, database, 1396, "Fixture")
			for _, e := range c.Episodes {
				id := videotest.SeedEpisode(t, database, show, e.Season, e.Number, e.AirDate)
				if e.Watched {
					videotest.SeedWatch(t, database, user, "episode", id, videotest.StatusNow)
				}
			}
			if c.Current != nil {
				videotest.SeedShowStatus(t, database, user, show, *c.Current)
			}

			videotest.Recompute(t, database, user, show)

			want := ""
			if c.Want != nil {
				want = *c.Want
			}
			if got := videotest.ShowStatus(t, database, user, show); got != want {
				t.Errorf("status = %q, want %q", got, want)
			}
		})
	}
}
