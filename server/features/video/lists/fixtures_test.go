package lists

import (
	"context"
	"net/http"
	"testing"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/refresh"
	"mbyte.ro/sofalog/features/video/tmdbaccount"
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

func movieIDs(refs []refresh.MovieRef) []int64 {
	out := make([]int64, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out
}

// syncService is a details service with one user and the link-flow paths
// scripted, so a test links in one call.
func syncService(t *testing.T, stub *videotest.ProviderStub) (*Service, int64, *time.Time) {
	t.Helper()
	svc, clock := detailsService(t, stub)
	stub.Bodies["/3/authentication/token/new"] = `{"success":true,"request_token":"tok-1"}`
	stub.Bodies["/3/authentication/session/new"] = videotest.TMDBSessionNewBody
	stub.Bodies["/3/account"] = videotest.TMDBAccountBody
	stub.Bodies["/3/authentication/session"] = `{"success":true}`
	return svc, videotest.SeedUser(t, svc.DB, "alice"), clock
}

// linkOnly runs the whole link flow for userID and fails the test if it does
// not land linked. The link queues nothing and turns no switch on (D-036).
func linkOnly(t *testing.T, svc *Service, userID int64) {
	t.Helper()
	ctx := context.Background()
	_, flow, err := tmdbaccount.NewService(svc.Core).StartTMDBLink(ctx, userID)
	if err != nil {
		t.Fatalf("StartTMDBLink: %v", err)
	}
	if err := tmdbaccount.NewService(svc.Core).CompleteTMDBLink(ctx, userID, flow, flow.RequestToken, true); err != nil {
		t.Fatalf("CompleteTMDBLink: %v", err)
	}
}

// linkUser is linkOnly plus the setup with push everywhere and every switch
// on — what linking used to do by itself, and what the outbox tests need: a
// linked user whose existing items are queued and whose writes queue. A
// re-link of the same account keeps its setup, so then only the switches are
// (re)asserted.
func linkUser(t *testing.T, svc *Service, userID int64) {
	t.Helper()
	linkOnly(t, svc, userID)
	st, err := tmdbaccount.NewService(svc.Core).TMDBLinkStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("TMDBLinkStatus: %v", err)
	}
	if !st.SetupDone {
		setupPushAll(t, svc, userID)
		return
	}
	if _, err := tmdbaccount.NewService(svc.Core).SetTMDBSyncKinds(context.Background(), userID, tmdbaccount.TMDBSyncKinds{Ratings: true, Watchlist: true, Favorites: true}); err != nil {
		t.Fatalf("SetTMDBSyncKinds: %v", err)
	}
}

// setupPushAll runs the setup with push on every kind — the former link-time
// backfill — and every switch on.
func setupPushAll(t *testing.T, svc *Service, userID int64) tmdbaccount.TMDBLinkStatus {
	t.Helper()
	push := tmdbaccount.TMDBSetupSection{Action: tmdbaccount.TMDBSetupPush, Sync: true}
	status, err := tmdbaccount.NewService(svc.Core).SetupTMDBSync(context.Background(), userID, tmdbaccount.TMDBSetupParams{Ratings: push, Watchlist: push, Favorites: push})
	if err != nil {
		t.Fatalf("SetupTMDBSync: %v", err)
	}
	return status
}
