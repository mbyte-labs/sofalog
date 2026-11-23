package upcoming

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"mbyte.ro/sofalog/features/video/videotest"
)

// The shared rule fixtures (SPEC D-040): the Android app runs these same cases
// against its Kotlin port of the calendar, so the two cannot drift. Only the
// entry order is checked here; the fold into cards is the clients' work.
func TestUpcomingFixtures(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../fixtures/video-rules/upcoming.json")
	if err != nil {
		t.Fatalf("reading fixtures: %v", err)
	}
	var file struct {
		Cases []struct {
			Name  string `json:"name"`
			Days  int    `json:"days"`
			Type  string `json:"type"`
			Shows []struct {
				Title       string  `json:"title"`
				Status      *string `json:"status"`
				Watchlisted bool    `json:"watchlisted"`
				Episodes    []struct {
					Season  int     `json:"season"`
					Number  int     `json:"number"`
					AirDate *string `json:"air_date"`
					Watched bool    `json:"watched"`
				} `json:"episodes"`
			} `json:"shows"`
			Movies []struct {
				Title       string  `json:"title"`
				ReleaseDate *string `json:"release_date"`
				Watchlisted bool    `json:"watchlisted"`
				Watched     bool    `json:"watched"`
			} `json:"movies"`
			Want []string `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decoding fixtures: %v", err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("no fixture cases")
	}
	const at = "2026-08-01T20:00:00Z"
	for _, c := range file.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			svc := upcomingService(t)
			user := videotest.SeedUser(t, svc.DB, "a@example.com")
			for i, s := range c.Shows {
				show := videotest.SeedShow(t, svc.DB, int64(100+i), s.Title)
				for _, e := range s.Episodes {
					id := videotest.SeedEpisode(t, svc.DB, show, e.Season, e.Number, e.AirDate)
					if e.Watched {
						videotest.SeedWatch(t, svc.DB, user, "episode", id, at)
					}
				}
				if s.Status != nil {
					videotest.SeedShowStatus(t, svc.DB, user, show, *s.Status)
				}
				if s.Watchlisted {
					videotest.SeedWatchlist(t, svc.DB, user, "show", show, at)
				}
			}
			for i, m := range c.Movies {
				movie := videotest.SeedMovie(t, svc.DB, int64(900+i), m.Title, m.ReleaseDate, nil, `{}`)
				if m.Watchlisted {
					videotest.SeedWatchlist(t, svc.DB, user, "movie", movie.ID, at)
				}
				if m.Watched {
					videotest.SeedWatch(t, svc.DB, user, "movie", movie.ID, at)
				}
			}

			p := UpcomingParams{Type: c.Type}
			if c.Days != 0 {
				p.Days = strconv.Itoa(c.Days)
			}
			wantKeys(t, upcoming(t, svc, user, p), c.Want...)
		})
	}
}
