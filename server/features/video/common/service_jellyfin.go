package common

import (
	"context"
	"slices"
)

// JellyfinSynced reports whether the scan owns the jellyfin copy kind — true
// exactly when a server is configured. Nil-safe for a hand-built test Service.
func (s *Core) JellyfinSynced() bool {
	return s.Jellyfin != nil && s.Jellyfin.Configured()
}

// MergeJellyfin is the one rule for reading the scan into a copies list. When
// the scan owns the kind, a hand-set `jellyfin` row is ignored (it predates
// the server, or somebody wrote it around the API) and the scan's answer is
// what the list says. Keeps the store's kind order.
func MergeJellyfin(kinds []string, present bool) []string {
	out := make([]string, 0, len(kinds)+1)
	for _, k := range kinds {
		if k != "jellyfin" {
			out = append(out, k)
		}
	}
	if present {
		out = append(out, "jellyfin")
		slices.Sort(out)
	}
	return out
}

// CopiesFor is what every details projection calls for a movie's or a
// season's copies: the user's own kinds, with the jellyfin kind taken from
// the scan when a server is configured. Always a non-nil slice. The second
// value is the season's episode-file count on the server — nil when the scan
// does not own the kind or the target is not there, so a page can tell a
// complete season from a partial one; always nil for a movie.
func (s *Core) CopiesFor(ctx context.Context, userID int64, targetType string, targetID int64) ([]string, *int, error) {
	kinds, err := UserTargetCopies(ctx, s.DB, userID, targetType, targetID)
	if err != nil {
		return nil, nil, err
	}
	if !s.JellyfinSynced() {
		return kinds, nil, nil
	}
	present, episodes, err := jellyfinEntryFor(ctx, s.DB, targetType, targetID)
	if err != nil {
		return nil, nil, err
	}
	var count *int
	if present && targetType == "season" {
		count = &episodes
	}
	return MergeJellyfin(kinds, present), count, nil
}

// SeasonCopiesFor is CopiesFor for the show page's per-season map. The
// second map carries the episode-file count of every season the server holds,
// keyed by season number; empty when the scan does not own the kind.
func (s *Core) SeasonCopiesFor(ctx context.Context, userID, showTMDBID int64) (map[int][]string, map[int]int, error) {
	copies, err := seasonCopiesByShow(ctx, s.DB, userID, showTMDBID)
	if err != nil {
		return nil, nil, err
	}
	if !s.JellyfinSynced() {
		return copies, map[int]int{}, nil
	}
	present, err := jellyfinSeasonsByShow(ctx, s.DB, showTMDBID)
	if err != nil {
		return nil, nil, err
	}
	merged := make(map[int][]string, len(copies)+len(present))
	for number, kinds := range copies {
		_, ok := present[number]
		merged[number] = MergeJellyfin(kinds, ok)
	}
	for number := range present {
		if _, ok := merged[number]; !ok {
			merged[number] = []string{"jellyfin"}
		}
	}
	return merged, present, nil
}
