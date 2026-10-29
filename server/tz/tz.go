// Package tz is the one rule for naming a time zone (D-038): an IANA name such
// as "Europe/Bucharest" or "UTC". Config validates DEFAULT_TIMEZONE with it, auth
// validates a user's choice with it, and the video feature resolves both
// through it — so a name accepted anywhere loads everywhere.
//
// Importing time/tzdata here embeds the zone database in the binary, so a name
// never depends on whether the host (or a scratch container) ships zoneinfo.
package tz

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

// ErrInvalid is returned for a name that is not a loadable IANA zone.
var ErrInvalid = errors.New("not a known IANA time zone")

var cache sync.Map // name → *time.Location

// Load resolves an IANA zone name. "Local" is refused: it means "whatever the
// host is set to", which is exactly the implicit zone this package exists to
// replace. The empty string is refused too, because time.LoadLocation reads it
// as UTC and a blank setting should never silently mean something.
func Load(name string) (*time.Location, error) {
	if v, ok := cache.Load(name); ok {
		return v.(*time.Location), nil
	}
	if strings.TrimSpace(name) == "" || name == "Local" {
		return nil, fmt.Errorf("%q: %w", name, ErrInvalid)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", name, ErrInvalid)
	}
	cache.Store(name, loc)
	return loc, nil
}

// Day is the calendar day t falls on in loc, as YYYY-MM-DD — the form every
// stored calendar date (air_date, release_date) takes, so the two compare as
// plain strings.
func Day(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(time.DateOnly)
}

// Midnight is the UTC instant at which day begins in loc, as the RFC3339 string
// every stored _at column holds. A day whose midnight a DST jump skips starts at
// the first instant that exists, which is what time.Date normalises to.
func Midnight(day string, loc *time.Location) (string, error) {
	d, err := time.ParseInLocation(time.DateOnly, day, loc)
	if err != nil {
		return "", err
	}
	return d.UTC().Format(time.RFC3339), nil
}
