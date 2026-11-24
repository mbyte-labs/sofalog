package stats

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/videotest"
)

// The shared rule fixtures (SPEC D-040): the Android app computes the Stats
// page on the device with a Kotlin port of this package's fold, and runs these
// same cases (StatsFixturesTest), so the two cannot drift.
//
// A case seeds a small library, asks for one period and states the sections it
// cares about as the response's own JSON. The comparison is a SUBSET: only the
// keys a case names are checked, objects recursively and lists element for
// element. A title is compared by kind, name and year — ids are local to each
// side, and the artwork fields differ by design.

type statsFixtureMovie struct {
	Key         string   `json:"key"`
	Title       string   `json:"title"`
	ReleaseDate *string  `json:"release_date"`
	Runtime     *int     `json:"runtime"`
	Genres      []string `json:"genres"`
	VoteAverage *float64 `json:"vote_average"`
	VoteCount   *int64   `json:"vote_count"`
}

type statsFixtureEpisode struct {
	Key     string  `json:"key"`
	Season  int     `json:"season"`
	Number  int     `json:"number"`
	AirDate *string `json:"air_date"`
	Runtime *int    `json:"runtime"`
}

type statsFixtureShow struct {
	Key            string                `json:"key"`
	Title          string                `json:"title"`
	FirstAirDate   *string               `json:"first_air_date"`
	Genres         []string              `json:"genres"`
	EpisodeRunTime *int                  `json:"episode_run_time"`
	VoteAverage    *float64              `json:"vote_average"`
	VoteCount      *int64                `json:"vote_count"`
	Status         *string               `json:"status"`
	Episodes       []statsFixtureEpisode `json:"episodes"`
}

type statsFixtureCase struct {
	Name   string              `json:"name"`
	Zone   string              `json:"zone"`
	Now    *string             `json:"now"`
	Period string              `json:"period"`
	Movies []statsFixtureMovie `json:"movies"`
	Shows  []statsFixtureShow  `json:"shows"`
	Plays  []struct {
		Target string `json:"target"`
		At     string `json:"at"`
	} `json:"plays"`
	Ratings []struct {
		Target string `json:"target"`
		Rating int    `json:"rating"`
		At     string `json:"at"`
	} `json:"ratings"`
	Want map[string]any `json:"want"`
	// The heatmap is a year of cells, so a case names only the days it cares
	// about: day -> [plays, level]. Every other cell must be empty.
	WantHeatmap *struct {
		Start string           `json:"start"`
		Days  int              `json:"days"`
		Cells map[string][]int `json:"cells"`
	} `json:"want_heatmap"`
}

func TestStatsFixtures(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../fixtures/video-rules/stats.json")
	if err != nil {
		t.Fatalf("reading fixtures: %v", err)
	}
	var file struct {
		Now   string             `json:"now"`
		Cases []statsFixtureCase `json:"cases"`
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
			at := file.Now
			if c.Now != nil {
				at = *c.Now
			}
			now, err := time.Parse(time.RFC3339, at)
			if err != nil {
				t.Fatalf("fixture now: %v", err)
			}
			svc := statsServiceAt(t, func() time.Time { return now })
			user := videotest.SeedUser(t, svc.DB, "a@example.com")
			videotest.SetUserTimezone(t, svc.DB, user, c.Zone)
			seedStatsFixture(t, svc, user, c)

			got := statsFor(t, svc, user, c.Period)
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("encoding stats: %v", err)
			}
			var tree any
			if err := json.Unmarshal(encoded, &tree); err != nil {
				t.Fatalf("decoding stats: %v", err)
			}
			if len(c.Want) == 0 && c.WantHeatmap == nil {
				t.Fatal("the case asserts nothing")
			}
			if diff := subsetDiff("stats", c.Want, normaliseStatsTitles(tree)); diff != "" {
				t.Error(diff)
			}
			if w := c.WantHeatmap; w != nil {
				h := got.Activity.Heatmap
				if h.Start != w.Start || len(h.Plays) != w.Days || len(h.Levels) != w.Days {
					t.Fatalf("heatmap = %s × %d, want %s × %d", h.Start, len(h.Plays), w.Start, w.Days)
				}
				start, err := time.Parse(time.DateOnly, h.Start)
				if err != nil {
					t.Fatalf("heatmap start: %v", err)
				}
				for i := range h.Plays {
					day := start.AddDate(0, 0, i).Format(time.DateOnly)
					want := []int{0, 0}
					if cell, ok := w.Cells[day]; ok {
						want = cell
					}
					if h.Plays[i] != want[0] || h.Levels[i] != want[1] {
						t.Errorf("heatmap %s = [%d, %d], want %v", day, h.Plays[i], h.Levels[i], want)
					}
				}
			}
		})
	}
}

// seedStatsFixture writes one case's library: catalog rows first, in the
// file's order (so ids rise the way the Kotlin side numbers them), then the
// log, the ratings and the show states.
func seedStatsFixture(t *testing.T, svc *Service, user int64, c statsFixtureCase) {
	t.Helper()
	type target struct {
		kind string
		id   int64
	}
	targets := map[string]target{}
	genres := func(names []string) *string {
		if names == nil {
			return nil
		}
		b, _ := json.Marshal(names)
		return videotest.Ptr(string(b))
	}
	body := func(avg *float64, votes *int64, runtime *int) string {
		doc := map[string]any{}
		if avg != nil {
			doc["vote_average"] = *avg
		}
		if votes != nil {
			doc["vote_count"] = *votes
		}
		if runtime != nil {
			doc["episode_run_time"] = []int{*runtime}
		}
		b, _ := json.Marshal(doc)
		return string(b)
	}
	for i, m := range c.Movies {
		row := videotest.SeedMovie(t, svc.DB, int64(1000+i), m.Title, m.ReleaseDate, nil, body(m.VoteAverage, m.VoteCount, nil))
		setMovieFacts(t, svc.DB, row.ID, m.Runtime, genres(m.Genres))
		targets[m.Key] = target{"movie", row.ID}
	}
	for i, sh := range c.Shows {
		id := videotest.SeedShow(t, svc.DB, int64(2000+i), sh.Title)
		setShowFacts(t, svc.DB, id, sh.FirstAirDate, genres(sh.Genres), videotest.Ptr(body(sh.VoteAverage, sh.VoteCount, sh.EpisodeRunTime)))
		targets[sh.Key] = target{"show", id}
		for _, e := range sh.Episodes {
			episode := videotest.SeedEpisode(t, svc.DB, id, e.Season, e.Number, e.AirDate)
			setEpisodeRuntime(t, svc.DB, episode, e.Runtime)
			targets[e.Key] = target{"episode", episode}
		}
		if sh.Status != nil {
			videotest.SeedShowStatus(t, svc.DB, user, id, *sh.Status)
		}
	}
	lookup := func(key string) target {
		tg, ok := targets[key]
		if !ok {
			t.Fatalf("fixture names an unknown target %q", key)
		}
		return tg
	}
	for _, p := range c.Plays {
		tg := lookup(p.Target)
		videotest.SeedWatch(t, svc.DB, user, tg.kind, tg.id, p.At)
	}
	for _, r := range c.Ratings {
		tg := lookup(r.Target)
		seedRatingAt(t, svc.DB, user, tg.kind, tg.id, r.Rating, r.At)
	}
}

// normaliseStatsTitles replaces every title object by its kind, name and year.
func normaliseStatsTitles(v any) any {
	switch node := v.(type) {
	case map[string]any:
		_, hasKind := node["kind"]
		_, hasTitle := node["title"]
		_, hasID := node["id"]
		if hasKind && hasTitle && hasID {
			return map[string]any{"kind": node["kind"], "title": node["title"], "year": node["year"]}
		}
		out := make(map[string]any, len(node))
		for k, child := range node {
			out[k] = normaliseStatsTitles(child)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, child := range node {
			out[i] = normaliseStatsTitles(child)
		}
		return out
	default:
		return v
	}
}

// subsetDiff reports the first place got does not carry what want states, or
// "" when it does. Numbers compare within 1e-9: the two sides divide the same
// integers, but nothing promises the last bit.
func subsetDiff(path string, want, got any) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s = %v, want an object", path, got)
		}
		for k, child := range w {
			value, present := g[k]
			if !present {
				return fmt.Sprintf("%s.%s is missing", path, k)
			}
			if d := subsetDiff(path+"."+k, child, value); d != "" {
				return d
			}
		}
		return ""
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s = %v, want %v", path, got, want)
		}
		for i := range w {
			if d := subsetDiff(fmt.Sprintf("%s[%d]", path, i), w[i], g[i]); d != "" {
				return d
			}
		}
		return ""
	case float64:
		g, ok := got.(float64)
		if !ok || math.Abs(g-w) > 1e-9 {
			return fmt.Sprintf("%s = %v, want %v", path, got, want)
		}
		return ""
	default:
		if want != got {
			return fmt.Sprintf("%s = %v, want %v", path, got, want)
		}
		return ""
	}
}
