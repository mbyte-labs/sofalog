package tmdbaccount

import (
	"context"
	"testing"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/restore"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/features/video/watches"
	"mbyte.ro/sofalog/provider"
)

// detailsService wires a Service against a stubbed provider, a temp DB and a
// fixed clock the test can move. The one stub backs both clients — it keys on
// the request path, and TMDB's and YouTube's never collide.
func detailsService(t *testing.T, stub *videotest.ProviderStub) (*Service, *time.Time) {
	t.Helper()
	if stub.Bodies == nil {
		stub.Bodies = map[string]string{}
	}
	tmdb := provider.NewTMDB(provider.TMDBCredentials{APIKeyV3: "test-key"}, provider.WithHTTPDoer(stub))
	youtube := provider.NewYouTube(provider.WithYouTubeHTTPDoer(stub))
	database, dataDir := videotest.DB(t), t.TempDir()
	cache, tree := assets.NewCache(dataDir), assets.NewTree(dataDir)
	svc := NewService(common.NewCore(database, tmdb, cache, youtube,
		tree, assets.NewQueue(database, tree, cache, nil), videotest.WatchRegion, 0, 0, videotest.LinkRedirectURI,
		provider.NewJellyfin("", ""), 0))
	clock := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return clock }
	return svc, &clock
}

// Entry builders: the file's shape, without the noise of every pointer.

func tbAddr(typ string, tmdb int64) restore.TicketBoxAddress {
	return restore.TicketBoxAddress{Type: typ, TMDBID: &tmdb, IMDBID: videotest.Ptr("tt0133093")}
}

func tbRating(typ string, tmdb int64, rating int, at string) restore.TicketBoxRatingEntry {
	return restore.TicketBoxRatingEntry{TicketBoxAddress: tbAddr(typ, tmdb), Rating: &rating, RatedAt: at}
}

func tbWatchlist(typ string, tmdb int64, at string) restore.TicketBoxWatchlistEntry {
	return restore.TicketBoxWatchlistEntry{TicketBoxAddress: tbAddr(typ, tmdb), ListedAt: at}
}

func tbFavorite(typ string, tmdb int64, at string) restore.TicketBoxFavoriteEntry {
	return restore.TicketBoxFavoriteEntry{TicketBoxAddress: tbAddr(typ, tmdb), FavoritedAt: at}
}

func tbDump(history []restore.TicketBoxHistoryEntry, ratings []restore.TicketBoxRatingEntry, watchlist []restore.TicketBoxWatchlistEntry) restore.TicketBoxDump {
	return restore.TicketBoxDump{SchemaVersion: 5, ExportedAt: "2026-08-19T10:00:00.000Z",
		History: history, Ratings: ratings, Watchlist: watchlist}
}

// tbFavorites is tbDump with a favorites section, the one array most cases
// do not need.
func tbFavorites(d restore.TicketBoxDump, favorites ...restore.TicketBoxFavoriteEntry) restore.TicketBoxDump {
	d.Favorites = favorites
	return d
}

// runRestore drives the import synchronously against svc and returns the
// final snapshot — the runner is bypassed, so nothing here is asynchronous.
func runRestore(t *testing.T, svc *Service, userID int64, dump restore.TicketBoxDump) restore.RestoreJob {
	t.Helper()
	now := svc.Now().UTC()
	plan, skips := restore.ValidateTicketBoxDump(dump, now)
	job := restore.NewRestoreJob(now, plan, skips)
	restore.NewService(svc.Core).RunTicketBoxRestore(context.Background(), userID, plan, job)
	return job.Snapshot()
}

// watchNow is the watch body for "record a watch, now" — the mode a caller
// gets by omitting watched_at. Most tests predate dated logging and only care
// about the target, so they say this rather than restating the wrapper.
func watchNow(p common.TrackParams) watches.WatchParams {
	return watches.WatchParams{TrackParams: p}
}
