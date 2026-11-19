package tmdbaccount

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The shared rule fixtures (SPEC D-040): the Android app links a TMDB account
// on the device and runs these same cases against its Kotlin port of the
// rating rounding and the outbox backoff, so the two cannot drift.
func TestTMDBSyncRuleFixtures(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../../../fixtures/video-rules/tmdb-sync.json")
	if err != nil {
		t.Fatalf("reading fixtures: %v", err)
	}
	var file struct {
		Rounding []struct {
			Value float64 `json:"value"`
			Want  int     `json:"want"`
		} `json:"rating_rounding"`
		Backoff []struct {
			Attempts int `json:"attempts"`
			Seconds  int `json:"seconds"`
		} `json:"backoff"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decoding fixtures: %v", err)
	}
	if len(file.Rounding) == 0 || len(file.Backoff) == 0 {
		t.Fatal("no fixture cases")
	}
	for _, c := range file.Rounding {
		if got := roundTMDBRating(c.Value); got != c.Want {
			t.Errorf("roundTMDBRating(%v) = %d, want %d", c.Value, got, c.Want)
		}
	}
	for _, c := range file.Backoff {
		want := time.Duration(c.Seconds) * time.Second
		if got := tmdbSyncBackoff(c.Attempts); got != want {
			t.Errorf("tmdbSyncBackoff(%d) = %v, want %v", c.Attempts, got, want)
		}
	}
}
