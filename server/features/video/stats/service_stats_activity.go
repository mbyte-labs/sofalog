package stats

import (
	"context"
	"sort"
	"time"

	"mbyte.ro/sofalog/tz"
)

// activity is the "Watch activity" section: every bucket is the play's instant
// read in the user's zone (D-038) — a play at 23:30 belongs to the day the
// user experienced, not to the UTC date. Day buckets are built with time.Date
// in that zone and then named through tz.Day, so a DST change can neither
// shift a bucket onto a neighbouring day nor make one 23 or 25 hours wide in
// the day arithmetic, which runs on the named days in UTC.
func (c *calc) activity(ctx context.Context, plays []playRow) (StatsActivity, error) {
	a := StatsActivity{Months: c.monthlyPlays(plays)}
	a.Heatmap = c.heatmap(plays)
	a.Weekday, a.TimeOfDay = c.weekdayAndTimeOfDay(plays)
	a.LongestStreak, a.CurrentStreak = c.streaks(plays)
	if c.per.all && len(plays) > 0 {
		if first, ok := c.localDay(plays[0].WatchedAt); ok {
			days := daysBetween(first, c.today)
			if days < 1 {
				days = 1
			}
			a.TrackingDays = &days
		}
	}
	if !c.per.all {
		active := 0
		for _, n := range a.Heatmap.Plays {
			if n > 0 {
				active++
			}
		}
		a.ActiveDays = &active
	}
	first, err := c.firstPlay(ctx, plays)
	if err != nil {
		return StatsActivity{}, err
	}
	a.FirstPlay = first
	return a, nil
}

// monthlyPlays counts plays into the page's twelve months; a play outside the
// window (all time, older than a year) is simply in no bucket.
func (c *calc) monthlyPlays(plays []playRow) []StatsMonthPlays {
	window := c.monthWindow()
	index := make(map[string]int, len(window))
	out := make([]StatsMonthPlays, 0, len(window))
	for i, m := range window {
		index[m] = i
		out = append(out, StatsMonthPlays{Month: m})
	}
	for _, r := range plays {
		if m, ok := c.localMonth(r.WatchedAt); ok {
			if i, ok := index[m]; ok {
				out[i].Plays++
			}
		}
	}
	return out
}

// heatmap is one cell per day: the 365 days ending today for all time, the
// whole calendar year otherwise. Shades come from the quartiles of the ACTIVE
// days' counts rather than from a share of the maximum, so one bulk-import day
// does not wash out every other day; the busiest day is always the top shade.
func (c *calc) heatmap(plays []playRow) StatsHeatmap {
	var start time.Time
	var n int
	if c.per.all {
		local := c.now.In(c.loc)
		start = time.Date(local.Year(), local.Month(), local.Day()-(heatmapRollingDay-1), 0, 0, 0, 0, c.loc)
		n = heatmapRollingDay
	} else {
		start = time.Date(c.per.year, time.January, 1, 0, 0, 0, 0, c.loc)
		n = time.Date(c.per.year, time.December, 31, 0, 0, 0, 0, c.loc).YearDay()
	}
	index := make(map[string]int, n)
	for i := 0; i < n; i++ {
		day := tz.Day(time.Date(start.Year(), start.Month(), start.Day()+i, 0, 0, 0, 0, c.loc), c.loc)
		index[day] = i
	}
	h := StatsHeatmap{Start: tz.Day(start, c.loc), Plays: make([]int, n), Levels: make([]int, n)}
	for _, r := range plays {
		if day, ok := c.localDay(r.WatchedAt); ok {
			if i, ok := index[day]; ok {
				h.Plays[i]++
			}
		}
	}
	h.Levels = heatLevels(h.Plays)
	return h
}

// heatLevels maps counts to shades 0..4: 0 for none; otherwise 1 plus the
// number of quartile thresholds strictly below the count, the thresholds
// being the active counts at index ⌊n·k/4⌋ for k = 1, 2, 3 of the sorted
// active counts, and the busiest day forced to 4.
func heatLevels(plays []int) []int {
	var active []int
	max := 0
	for _, n := range plays {
		if n > 0 {
			active = append(active, n)
			if n > max {
				max = n
			}
		}
	}
	sort.Ints(active)
	levels := make([]int, len(plays))
	if len(active) == 0 {
		return levels
	}
	n := len(active)
	thresholds := []int{active[n*1/4], active[n*2/4], active[n*3/4]}
	for i, count := range plays {
		switch {
		case count == 0:
			levels[i] = 0
		case count == max:
			levels[i] = 4
		default:
			lvl := 1
			for _, t := range thresholds {
				if t < count {
					lvl++
				}
			}
			levels[i] = lvl
		}
	}
	return levels
}

// weekdayAndTimeOfDay buckets plays by local weekday (Monday first) and by
// part of the local day. A play at EXACTLY local midnight is left out of the
// time-of-day chart: the bulk "mark on air dates" modes stamp that instant
// (D-038), which would make everyone look like a night owl. The chart is
// null when no play has a real time of day; the dominant part is the fullest,
// a tie going to the earlier one.
func (c *calc) weekdayAndTimeOfDay(plays []playRow) ([7]int, *StatsTimeOfDay) {
	var weekday [7]int
	var parts [4]int
	timed := 0
	for _, r := range plays {
		t, ok := parseInstant(r.WatchedAt)
		if !ok {
			continue
		}
		lt := t.In(c.loc)
		weekday[(int(lt.Weekday())+6)%7]++
		if lt.Hour() == 0 && lt.Minute() == 0 && lt.Second() == 0 && lt.Nanosecond() == 0 {
			continue
		}
		timed++
		switch h := lt.Hour(); {
		case h >= 5 && h < 12:
			parts[0]++
		case h >= 12 && h < 17:
			parts[1]++
		case h >= 17 && h < 22:
			parts[2]++
		default:
			parts[3]++
		}
	}
	if timed == 0 {
		return weekday, nil
	}
	names := [4]string{"morning", "afternoon", "evening", "night"}
	dominant := 0
	for i := 1; i < 4; i++ {
		if parts[i] > parts[dominant] {
			dominant = i
		}
	}
	return weekday, &StatsTimeOfDay{
		Morning: parts[0], Afternoon: parts[1], Evening: parts[2], Night: parts[3],
		Dominant: names[dominant],
	}
}

// streaks finds the longest run of consecutive local days with a play, and
// the current one: the run ending on the latest play day, counted only if
// that day is today or yesterday (today just hasn't happened yet). The
// current streak is null in a past year, which has no "current" facts.
func (c *calc) streaks(plays []playRow) (longest int, current *int) {
	seen := map[string]bool{}
	var days []string
	for _, r := range plays {
		if day, ok := c.localDay(r.WatchedAt); ok && !seen[day] {
			seen[day] = true
			days = append(days, day)
		}
	}
	sort.Strings(days)
	run := 0
	for i, day := range days {
		if i > 0 && daysBetween(days[i-1], day) == 1 {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
	}
	if c.per.past {
		return longest, nil
	}
	cur := 0
	if len(days) > 0 {
		last := days[len(days)-1]
		local := c.now.In(c.loc)
		yesterday := tz.Day(time.Date(local.Year(), local.Month(), local.Day()-1, 0, 0, 0, 0, c.loc), c.loc)
		if last == c.today || last == yesterday {
			cur = run
		}
	}
	return longest, &cur
}

// daysBetween counts the calendar days from one YYYY-MM-DD to another, in UTC
// date space, so no zone or DST offset can make a day 23 or 25 hours.
func daysBetween(from, to string) int {
	a, errA := time.Parse(time.DateOnly, from)
	b, errB := time.Parse(time.DateOnly, to)
	if errA != nil || errB != nil {
		return 0
	}
	return int(b.Sub(a).Hours() / 24)
}

// firstPlay is the oldest play of the period — the bulk read's order puts a
// tie on the instant at the lower id. An episode play refers to its show and
// carries its position; a movie play, the movie.
func (c *calc) firstPlay(ctx context.Context, plays []playRow) (*StatsFirstPlay, error) {
	for _, r := range plays {
		var title *StatsTitle
		var err error
		var ep *StatsEpisodeRef
		if r.TargetType == "episode" {
			if r.ShowID == nil {
				continue
			}
			title, err = c.titles.show(ctx, *r.ShowID)
			if r.Season != nil && r.Number != nil {
				ep = &StatsEpisodeRef{Season: *r.Season, Number: *r.Number}
			}
		} else {
			title, err = c.titles.movie(ctx, r.TargetID)
		}
		if err != nil {
			return nil, err
		}
		if title == nil {
			continue
		}
		return &StatsFirstPlay{Title: *title, Episode: ep, WatchedAt: r.WatchedAt}, nil
	}
	return nil, nil
}
