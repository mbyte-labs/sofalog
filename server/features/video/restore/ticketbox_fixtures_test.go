package restore

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/common"
)

// The shared rule fixtures (SPEC D-040): the Android app restores a Ticket Box
// file on the device and runs these same cases against its Kotlin port of the
// instant rule, the validation and the episode hint, so the two cannot drift.
func TestTicketBoxRuleFixtures(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../fixtures/video-rules/ticket-box-restore.json")
	if err != nil {
		t.Fatalf("reading fixtures: %v", err)
	}
	type skip struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	}
	var file struct {
		Now      time.Time `json:"now"`
		Instants []struct {
			Raw    string  `json:"raw"`
			Want   *string `json:"want"`
			Reason string  `json:"reason"`
		} `json:"instants"`
		Validate []struct {
			Name   string          `json:"name"`
			Dump   json.RawMessage `json:"dump"`
			Skips  []skip          `json:"skips"`
			Movies []int64         `json:"movies"`
			Shows  []int64         `json:"shows"`
		} `json:"validate"`
		Hints []struct {
			Numbers []int  `json:"numbers"`
			Season  int    `json:"season"`
			Episode int    `json:"episode"`
			Hint    string `json:"hint"`
			Last    *int   `json:"last"`
		} `json:"episode_hints"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decoding fixtures: %v", err)
	}
	if len(file.Instants) == 0 || len(file.Validate) == 0 || len(file.Hints) == 0 {
		t.Fatal("no fixture cases")
	}

	for _, c := range file.Instants {
		got, err := common.NormaliseInstant(c.Raw, file.Now)
		reason := ""
		switch {
		case err == nil:
		case errors.Is(err, common.ErrInstantFuture):
			reason = skipFutureTimestamp
		default:
			reason = skipInvalidTimestamp
		}
		if reason != c.Reason {
			t.Errorf("NormaliseInstant(%q) reason = %q, want %q", c.Raw, reason, c.Reason)
		}
		if c.Want != nil && got != *c.Want {
			t.Errorf("NormaliseInstant(%q) = %q, want %q", c.Raw, got, *c.Want)
		}
	}

	for _, c := range file.Validate {
		dump, err := parseTicketBoxDump(c.Dump)
		if err != nil {
			t.Fatalf("%s: parsing: %v", c.Name, err)
		}
		plan, skips := ValidateTicketBoxDump(dump, file.Now)
		if len(skips) != len(c.Skips) {
			t.Fatalf("%s: %d skips, want %d: %+v", c.Name, len(skips), len(c.Skips), skips)
		}
		for i, s := range skips {
			if s.Kind != c.Skips[i].Kind || s.Reason != c.Skips[i].Reason {
				t.Errorf("%s: skip %d = %s/%s, want %s/%s", c.Name, i, s.Kind, s.Reason, c.Skips[i].Kind, c.Skips[i].Reason)
			}
		}
		var movies, shows []int64
		for _, g := range plan.movies {
			movies = append(movies, g.tmdbID)
		}
		for _, g := range plan.shows {
			shows = append(shows, g.tmdbID)
		}
		if !equalIDs(movies, c.Movies) || !equalIDs(shows, c.Shows) {
			t.Errorf("%s: groups = movies %v shows %v, want movies %v shows %v", c.Name, movies, shows, c.Movies, c.Shows)
		}
	}

	for _, c := range file.Hints {
		hint, last := episodeSkipHint(c.Numbers, c.Season, c.Episode)
		sameLast := (last == nil) == (c.Last == nil) && (last == nil || *last == *c.Last)
		if hint != c.Hint || !sameLast {
			t.Errorf("episodeSkipHint(%v, %d, %d) = %s, want %s", c.Numbers, c.Season, c.Episode, hint, c.Hint)
		}
	}
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
