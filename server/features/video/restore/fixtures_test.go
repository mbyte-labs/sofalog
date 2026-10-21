package restore

import (
	"net/http"
	"testing"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/provider"
)

// trackingTestMux wires the routes against a stubbed provider and a temp DB
// with one seeded user, whose id every request carries — the handler reads it
// from the context exactly as the session middleware would set it.
func trackingTestMux(t *testing.T, stub *videotest.ProviderStub) (*http.ServeMux, int64) {
	t.Helper()
	if stub.Bodies == nil {
		stub.Bodies = map[string]string{}
	}
	tmdb := provider.NewTMDB(provider.TMDBCredentials{APIKeyV3: "test-key"}, provider.WithHTTPDoer(stub))
	database, dataDir := videotest.DB(t), t.TempDir()
	cache, tree := assets.NewCache(dataDir), assets.NewTree(dataDir)
	svc := NewService(common.NewCore(database, tmdb, cache, provider.NewYouTube(provider.WithYouTubeHTTPDoer(stub)),
		tree, assets.NewQueue(database, tree, cache, nil), videotest.WatchRegion, 0, 0, videotest.LinkRedirectURI,
		provider.NewJellyfin("", ""), 0))
	userID := videotest.SeedUser(t, svc.DB, "alice")
	mux := http.NewServeMux()
	allHandlers(svc, videotest.Cookie).RegisterRoutes(mux)
	return mux, userID
}

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
