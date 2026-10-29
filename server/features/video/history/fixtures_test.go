package history

import (
	"context"
	"testing"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
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

// trackedShowService materialises Breaking Bad and returns the service, the
// user and the show row — the starting point for every marking test.
func trackedShowService(t *testing.T) (*Service, int64, common.ShowRow) {
	t.Helper()
	stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
	svc, _ := detailsService(t, stub)
	userID := videotest.SeedUser(t, svc.DB, "alice")
	show, err := svc.MaterialiseShow(context.Background(), 1396)
	if err != nil {
		t.Fatalf("materialiseShow: %v", err)
	}
	return svc, userID, show
}

// watchNow is the watch body for "record a watch, now" — the mode a caller
// gets by omitting watched_at. Most tests predate dated logging and only care
// about the target, so they say this rather than restating the wrapper.
func watchNow(p common.TrackParams) watches.WatchParams {
	return watches.WatchParams{TrackParams: p}
}
