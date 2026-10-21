package common

import (
	"slices"
	"strings"
)

// CastEntry is one show cast member as the DTO and the asset set both read it:
// the two go through ShowCastEntries so the page and the downloaded photo set
// cannot disagree about who is in the cast.
type CastEntry struct {
	TMDBID      int64 // TMDB person id; 0 when the body has none
	Name        string
	Character   string // "" when TMDB names no part
	ProfilePath string // "/abc.jpg", or "" when there is no photo
	Order       int
	Episodes    *int // nil on the legacy credits shape
}

// ShowCastEntries reads a show snapshot's cast, in billing order with blank
// names dropped. It prefers aggregate_credits — everyone credited across every
// episode, the section TVDetails appends — and falls back to the `credits`
// section: for a show, the latest-season cast a snapshot written before the
// append was switched holds, so an old row keeps rendering when the one
// refresh the gate forces fails; for a season document (which folds its own
// `credits` in), the season's regulars when TMDB sent no aggregate section.
//
// Aggregate entries carry a roles array instead of a character: the parts are
// joined most-episodes-first with " / ", which is how TMDB's own page reads
// them. Billing ties break on total episodes, then keep TMDB's order.
func ShowCastEntries(snap MediaSnapshot) []CastEntry {
	var out []CastEntry
	if snap.AggregateCredits != nil {
		out = make([]CastEntry, 0, len(snap.AggregateCredits.Cast))
		for _, m := range snap.AggregateCredits.Cast {
			if m.Name == "" {
				continue
			}
			roles := slices.Clone(m.Roles)
			slices.SortStableFunc(roles, func(a, b snapshotCastRole) int {
				return b.EpisodeCount - a.EpisodeCount
			})
			parts := make([]string, 0, len(roles))
			for _, r := range roles {
				if r.Character != "" {
					parts = append(parts, r.Character)
				}
			}
			e := CastEntry{
				TMDBID:      m.ID,
				Name:        m.Name,
				Character:   strings.Join(parts, " / "),
				ProfilePath: m.ProfilePath,
				Order:       m.Order,
			}
			if m.TotalEpisodeCount > 0 {
				n := m.TotalEpisodeCount
				e.Episodes = &n
			}
			out = append(out, e)
		}
		slices.SortStableFunc(out, func(a, b CastEntry) int {
			if a.Order != b.Order {
				return a.Order - b.Order
			}
			return episodesOf(b) - episodesOf(a)
		})
		return out
	}

	return LegacyCastEntries(snap.Credits.Cast)
}

// LegacyCastEntries reads the plain credits shape — one part per actor, no
// episode counts — in billing order with blank names dropped. It serves a show
// snapshot written before aggregate credits were appended, a season's regular
// cast (its `credits` section), and an episode's guest stars.
func LegacyCastEntries(cast []SnapshotCastMember) []CastEntry {
	out := make([]CastEntry, 0, len(cast))
	for _, m := range cast {
		if m.Name == "" {
			continue
		}
		out = append(out, CastEntry{
			TMDBID:      m.ID,
			Name:        m.Name,
			Character:   m.Character,
			ProfilePath: m.ProfilePath,
			Order:       m.Order,
		})
	}
	slices.SortStableFunc(out, func(a, b CastEntry) int {
		return a.Order - b.Order
	})
	return out
}

func episodesOf(e CastEntry) int {
	if e.Episodes == nil {
		return 0
	}
	return *e.Episodes
}

// PosterProxyURL builds the relative cache-proxy URL for one TMDB image
// variant. Relative, like every asset URL the API returns (the SPA is served
// from BASE_URL and may sit under a sub-path).
func PosterProxyURL(size, filePath string) string {
	return "api/v1/video/posters/" + size + "/" + strings.TrimPrefix(filePath, "/")
}

// YearOf takes the year off a YYYY-MM-DD date, or nil when there is none.
func YearOf(date string) *string {
	if len(date) < 4 {
		return nil
	}
	year := date[:4]
	return &year
}
