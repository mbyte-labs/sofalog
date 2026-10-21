package media

import (
	"net/http"
	"testing"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
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
