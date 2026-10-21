package details

import (
	"encoding/json"

	"mbyte.ro/sofalog/features/video/common"
)

// watchProvidersStale reports whether a stored snapshot's provider section is
// missing or was pruned to a different country than the one now configured.
// Both mean the same thing to the caller — one refresh, on the next view —
// which is how a widened append and a changed TMDB_WATCH_REGION reach existing
// rows without a migration. A fresh fetch always writes the marker, so this
// fires at most once per row per change; if the refresh fails, the serve-stale
// path applies and the section renders empty.
func watchProvidersStale(metadataJSON, region string) bool {
	var probe struct {
		WatchProviders *common.PrunedWatchProviders `json:"watch/providers"`
	}
	if err := json.Unmarshal([]byte(metadataJSON), &probe); err != nil {
		return true
	}
	return probe.WatchProviders == nil || probe.WatchProviders.SofalogRegion != region
}
