package common_test

import (
	"context"
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/tz"
)

func TestClockFor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database := videotest.DB(t)
	bucharest, err := tz.Load("Europe/Bucharest")
	if err != nil {
		t.Fatal(err)
	}

	unset := videotest.SeedUser(t, database, "unset@example.com")
	ny := videotest.SeedUser(t, database, "ny@example.com")
	videotest.SetUserTimezone(t, database, ny, "America/New_York")
	utc := videotest.SeedUser(t, database, "utc@example.com")
	videotest.SetUserTimezone(t, database, utc, "UTC")
	stale := videotest.SeedUser(t, database, "stale@example.com")
	videotest.SetUserTimezone(t, database, stale, "Mars/Olympus_Mons")

	tests := []struct {
		name        string
		defaultZone *time.Location
		user        int64
		wantToday   string
	}{
		{"an unset user takes the server default", bucharest, unset, "2026-08-19"},
		{"a hand-built Core's default is UTC", nil, unset, "2026-08-18"},
		{"a chosen zone beats the default", bucharest, ny, "2026-08-18"},
		{"UTC is a zone like any other", bucharest, utc, "2026-08-18"},
		{"an unloadable stored name falls back to the default", bucharest, stale, "2026-08-19"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core := &common.Core{Now: videotest.LateEveningClock, DB: database, DefaultZone: tt.defaultZone}
			clk, err := core.ClockFor(ctx, tt.user)
			if err != nil {
				t.Fatalf("ClockFor: %v", err)
			}
			if clk.Today != tt.wantToday {
				t.Errorf("today = %q, want %q", clk.Today, tt.wantToday)
			}
			// The stored instant never moves with the zone.
			if clk.Now != "2026-08-18T22:30:00Z" {
				t.Errorf("now = %q, want the UTC instant", clk.Now)
			}
		})
	}
}
