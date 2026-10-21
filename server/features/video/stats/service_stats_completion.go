package stats

import (
	"context"
	"sort"
)

// completion summarises every followed, non-dropped show (all time only — the
// state table holds current facts without dates, so a year has no completion
// numbers): how many are finished, the mean completion ratio over shows with
// an aired episode (a show with none is tracked but left out of the mean), and
// the three closest to done — fewest episodes left, then the higher ratio,
// then the lowest id. Null when no show is followed.
func (c *calc) completion(ctx context.Context, rows []completionRow) (*StatsCompletion, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := &StatsCompletion{Tracked: len(rows), AlmostThere: make([]StatsAlmostThere, 0, almostThereLimit)}
	var sum float64
	rated := 0
	var open []completionRow
	for _, r := range rows {
		if r.Aired > 0 && r.Watched >= r.Aired {
			out.Completed++
		}
		if r.Aired > 0 {
			ratio := float64(r.Watched) / float64(r.Aired)
			if ratio > 1 {
				ratio = 1
			}
			sum += ratio
			rated++
			if r.Watched > 0 && r.Watched < r.Aired {
				open = append(open, r)
			}
		}
	}
	out.InProgress = out.Tracked - out.Completed
	if rated > 0 {
		out.AverageCompletion = sum / float64(rated)
	}
	sort.Slice(open, func(i, j int) bool {
		a, b := open[i], open[j]
		if la, lb := a.Aired-a.Watched, b.Aired-b.Watched; la != lb {
			return la < lb
		}
		if ra, rb := float64(a.Watched)/float64(a.Aired), float64(b.Watched)/float64(b.Aired); ra != rb {
			return ra > rb
		}
		return a.ShowID < b.ShowID
	})
	for _, r := range open {
		if len(out.AlmostThere) == almostThereLimit {
			break
		}
		title, err := c.titles.show(ctx, r.ShowID)
		if err != nil {
			return nil, err
		}
		if title == nil {
			continue
		}
		out.AlmostThere = append(out.AlmostThere, StatsAlmostThere{Show: *title, Watched: r.Watched, Aired: r.Aired, Left: r.Aired - r.Watched})
	}
	return out, nil
}
