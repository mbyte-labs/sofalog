package upnext

import (
	"encoding/json"
	"os"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

// The shared rule fixtures (SPEC D-040): the Android app runs these same cases
// against its Kotlin port of the next-episode pick, so the two cannot drift.
func TestUpNextPickFixtures(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../fixtures/video-rules/next-episode.json")
	if err != nil {
		t.Fatalf("reading fixtures: %v", err)
	}
	type position struct {
		Season int `json:"season"`
		Number int `json:"number"`
	}
	var file struct {
		Cases []struct {
			Name     string    `json:"name"`
			Dropped  bool      `json:"dropped"`
			Want     *position `json:"want"`
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
	if len(file.Cases) == 0 {
		t.Fatal("no fixture cases")
	}
	for _, c := range file.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			svc := upNextService(t)
			user := videotest.SeedUser(t, svc.DB, "a@example.com")
			show := videotest.SeedShow(t, svc.DB, 1396, "Fixture")
			for _, e := range c.Episodes {
				id := videotest.SeedEpisode(t, svc.DB, show, e.Season, e.Number, e.AirDate)
				if e.Watched {
					videotest.SeedWatch(t, svc.DB, user, "episode", id, videotest.StatusNow)
				}
			}
			if c.Dropped {
				videotest.SeedShowStatus(t, svc.DB, user, show, "dropped")
			}

			entries := upNext(t, svc, user, UpNextParams{})

			if c.Want == nil {
				if len(entries) != 0 {
					t.Fatalf("listed S%dE%d, want the show absent", entries[0].Episode.Season, entries[0].Episode.Number)
				}
				return
			}
			wantEpisode(t, oneEntry(t, entries), c.Want.Season, c.Want.Number)
		})
	}
}
