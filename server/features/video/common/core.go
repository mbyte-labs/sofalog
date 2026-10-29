package common

import (
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/provider"
)

// Core is what every video feature shares: the dependencies, the server-wide
// settings, and — as methods, across this package's files — the catalog
// operations more than one feature needs (tracking, snapshot refresh, asset
// sync, status recompute). Each feature's Service embeds a *Core.
type Core struct {
	// Now is injectable so tests of only_released and of snapshot staleness
	// are stable regardless of when they run.
	Now func() time.Time

	DB *db.DB

	// TMDB and Cache serve the search and details-snapshot flows only —
	// nothing on a pure render path may reach either provider traffic or
	// cache misses (D-015 is the one sanctioned exception).
	TMDB  *provider.TMDB
	Cache *assets.Cache

	// YouTube serves the media section's video thumbnails and nothing else.
	// It is an image source, not a metadata provider: TMDB names the videos,
	// YouTube only supplies their frames (D-017).
	YouTube *provider.YouTube

	// Tree holds tracked items' permanent artwork (data/assets); Queue is the
	// download worker that fills it. Tracking and refreshes enqueue through
	// SyncMovieAssets / SyncShowAssets — nothing here downloads inline.
	Tree  *assets.Tree
	Queue *assets.Queue

	// WatchRegion is the ISO-3166-1 alpha-2 country whose "where to watch"
	// listing the details views project (D-022). It never reaches TMDB — the
	// stored snapshot holds every country, so this only picks a key.
	WatchRegion string

	// TMDBLinkRedirectURI is where TMDB sends the browser back after the user
	// approves a link (D-029), derived from BASE_URL alone in config.
	TMDBLinkRedirectURI string

	// TMDBSyncKick wakes the TMDB outbox worker (D-029) after a commit; see
	// KickTMDBSync. Nil on a Core built by hand in a test, where every kick is
	// a no-op.
	TMDBSyncKick chan struct{}

	// Jellyfin is the household's Jellyfin server (D-033), read only by the
	// library scan — never on a render path. Unconfigured (or nil on a
	// hand-built test Core) means the jellyfin copy kind stays a hand-set
	// label.
	Jellyfin *provider.Jellyfin

	// RefreshEvery and ArchiveEvery are the two scheduled refresh jobs'
	// intervals (D-025), JellyfinEvery the library scan's cadence (D-033);
	// zero disables the job. Read once, when the owning feature is built.
	RefreshEvery, ArchiveEvery, JellyfinEvery time.Duration

	// DefaultZone is DEFAULT_TIMEZONE (D-038): the zone of a user who never
	// chose one. Nil on a Core built by hand in a test, where it reads as
	// UTC; read it through DefaultLocation, and a user's day through ClockFor.
	DefaultZone *time.Location
}

// NewCore builds the shared core from main's wiring.
func NewCore(database *db.DB, tmdb *provider.TMDB, cache *assets.Cache, youtube *provider.YouTube, tree *assets.Tree, queue *assets.Queue, watchRegion string, refreshEvery, archiveEvery time.Duration, tmdbLinkRedirectURI string, jellyfin *provider.Jellyfin, jellyfinEvery time.Duration) *Core {
	return &Core{Now: time.Now, DB: database, TMDB: tmdb, Cache: cache, YouTube: youtube, Tree: tree, Queue: queue,
		WatchRegion: watchRegion, TMDBLinkRedirectURI: tmdbLinkRedirectURI, TMDBSyncKick: make(chan struct{}, 1),
		Jellyfin: jellyfin, RefreshEvery: refreshEvery, ArchiveEvery: archiveEvery, JellyfinEvery: jellyfinEvery}
}
