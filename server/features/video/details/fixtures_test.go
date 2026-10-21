package details

import (
	"context"
	"net/http"
	"testing"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/features/video/watches"
	"mbyte.ro/sofalog/provider"
)

// searchTestMux wires a real mux against a stubbed provider and a temp-dir
// cache — no network, no shared state between tests.
func searchTestMux(t *testing.T, stub *videotest.ProviderStub) *http.ServeMux {
	t.Helper()
	if stub.Bodies == nil {
		stub.Bodies = map[string]string{}
	}
	if _, ok := stub.Bodies["/3/configuration"]; !ok {
		stub.Bodies["/3/configuration"] = videotest.TMDBConfigBody
	}
	tmdb := provider.NewTMDB(provider.TMDBCredentials{APIKeyV3: "test-key"}, provider.WithHTTPDoer(stub))
	mux := http.NewServeMux()
	database, dataDir := videotest.DB(t), t.TempDir()
	cache, tree := assets.NewCache(dataDir), assets.NewTree(dataDir)
	allHandlers(NewService(common.NewCore(database, tmdb, cache, provider.NewYouTube(provider.WithYouTubeHTTPDoer(stub)),
		tree, assets.NewQueue(database, tree, cache, nil), videotest.WatchRegion, 0, 0, videotest.LinkRedirectURI,
		provider.NewJellyfin("", ""), 0)), videotest.Cookie).RegisterRoutes(mux)
	return mux
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
