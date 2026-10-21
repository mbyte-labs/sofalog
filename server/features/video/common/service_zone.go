package common

import (
	"context"
	"time"

	"mbyte.ro/sofalog/tz"
)

// Clock is one reading of the real clock for one user (D-038): the instant
// every stored _at column takes (always UTC), and the calendar day it falls
// on in that user's zone — where "today" ends for D-012's aired predicate,
// Up Next's aired flag, the calendar's first day and the bulk air-date modes.
// Both come from the same reading, so they can never straddle a midnight.
type Clock struct {
	At    time.Time      // the instant, in UTC
	Zone  *time.Location // the user's zone, else the server default
	Now   string         // At as RFC3339 — what _at columns store
	Today string         // the user's calendar day, YYYY-MM-DD
}

// ClockFor reads the clock for userID. It is the one definition of a user's
// day: nothing in the video feature formats a DateOnly from a bare UTC
// instant any more.
func (c *Core) ClockFor(ctx context.Context, userID int64) (Clock, error) {
	name, err := userTimezone(ctx, c.DB, userID)
	if err != nil {
		return Clock{}, err
	}
	return c.ClockIn(c.Now(), name), nil
}

// ClockIn is ClockFor with the reading and the zone name already in hand —
// what the status sweep uses, reading the clock once for every user. An empty
// name is the server default.
func (c *Core) ClockIn(at time.Time, zoneName string) Clock {
	zone := c.ZoneNamed(zoneName)
	at = at.UTC()
	return Clock{At: at, Zone: zone, Now: at.Format(time.RFC3339), Today: tz.Day(at, zone)}
}

// ZoneNamed resolves a stored zone name, falling back to the server default
// for "" and for a name this build cannot load (auth validated it when it
// was written, so that is a tzdata change, not a user error — the day should
// still have an end).
func (c *Core) ZoneNamed(name string) *time.Location {
	if name != "" {
		if loc, err := tz.Load(name); err == nil {
			return loc
		}
	}
	return c.DefaultLocation()
}

// DefaultLocation is DEFAULT_TIMEZONE, or UTC on a Core built by hand in a
// test — the zone of every user who never chose one, and of the catalog-only
// work (the refresh job's open-season check) that has no user at all.
func (c *Core) DefaultLocation() *time.Location {
	if c.DefaultZone != nil {
		return c.DefaultZone
	}
	return time.UTC
}
