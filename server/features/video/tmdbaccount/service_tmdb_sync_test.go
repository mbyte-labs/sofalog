package tmdbaccount

import (
	"context"
	"errors"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/lists"
	"mbyte.ro/sofalog/features/video/ratings"
	"mbyte.ro/sofalog/features/video/restore"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/features/video/watches"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

const (
	tmdbRatingOKBody = `{"status_code":1,"status_message":"Success."}`
)

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
	_, flow, err := svc.StartTMDBLink(ctx, userID)
	if err != nil {
		t.Fatalf("StartTMDBLink: %v", err)
	}
	if err := svc.CompleteTMDBLink(ctx, userID, flow, flow.RequestToken, true); err != nil {
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
	st, err := svc.TMDBLinkStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("TMDBLinkStatus: %v", err)
	}
	if !st.SetupDone {
		setupPushAll(t, svc, userID)
		return
	}
	if _, err := svc.SetTMDBSyncKinds(context.Background(), userID, TMDBSyncKinds{Ratings: true, Watchlist: true, Favorites: true}); err != nil {
		t.Fatalf("SetTMDBSyncKinds: %v", err)
	}
}

// setupPushAll runs the setup with push on every kind — the former link-time
// backfill — and every switch on.
func setupPushAll(t *testing.T, svc *Service, userID int64) TMDBLinkStatus {
	t.Helper()
	push := TMDBSetupSection{Action: TMDBSetupPush, Sync: true}
	status, err := svc.SetupTMDBSync(context.Background(), userID, TMDBSetupParams{Ratings: push, Watchlist: push, Favorites: push})
	if err != nil {
		t.Fatalf("SetupTMDBSync: %v", err)
	}
	return status
}

func TestTMDBLinkFlow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("start returns the approval url with the callback and a bound flow", func(t *testing.T) {
		t.Parallel()
		svc, userID, clock := syncService(t, &videotest.ProviderStub{})
		target, flow, err := svc.StartTMDBLink(ctx, userID)
		if err != nil {
			t.Fatalf("StartTMDBLink: %v", err)
		}
		want := "https://www.themoviedb.org/authenticate/tok-1?redirect_to=http%3A%2F%2Fsofalog.test%2Fapi%2Fv1%2Fvideo%2Ftmdb%2Flink%2Fcallback"
		if target != want {
			t.Errorf("url = %q, want %q", target, want)
		}
		if flow.UserID != userID || flow.RequestToken != "tok-1" || !flow.Expires.Equal(clock.Add(tmdbLinkFlowTTL)) {
			t.Errorf("flow = %+v", flow)
		}
	})

	t.Run("complete stores the account and queues nothing until the setup", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)

		linkOnly(t, svc, userID)

		status, err := svc.TMDBLinkStatus(ctx, userID)
		if err != nil {
			t.Fatalf("TMDBLinkStatus: %v", err)
		}
		if !status.Linked || status.Broken || status.Username == nil || *status.Username != "bob" || !status.Configured {
			t.Errorf("status = %+v", status)
		}
		if status.SetupDone || status.Sync != (TMDBSyncKinds{}) || status.Pending != 0 || status.Pull != nil {
			t.Errorf("status = %+v, want no setup, no switches, nothing queued", status)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("outbox rows = %d, want 0 — the link queues nothing", n)
		}
		// A write before the setup queues nothing either: no switch is on.
		if _, err := ratings.NewService(svc.Core).Rate(ctx, userID, ratings.RateParams{TrackParams: common.TrackParams{TargetType: "movie", TargetID: &movie.ID}, Rating: videotest.Ptr(9)}); err != nil {
			t.Fatalf("Rate: %v", err)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("outbox rows after a pre-setup rating = %d, want 0", n)
		}
		if stub.CallCount("/3/account") != 1 {
			t.Errorf("account calls = %d, want 1", stub.CallCount("/3/account"))
		}
	})

	t.Run("the setup's push backfills every rateable rating", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		showID := videotest.SeedShow(t, svc.DB, 1399, "Thrones")
		epID := videotest.SeedEpisode(t, svc.DB, showID, 1, 1, videotest.Ptr("2011-04-17"))
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		videotest.SeedRating(t, svc.DB, userID, "show", showID, 9)
		videotest.SeedRating(t, svc.DB, userID, "episode", epID, 7)
		videotest.SeedRating(t, svc.DB, userID, "season", 5, 6) // D-020: never pushed
		other := videotest.SeedUser(t, svc.DB, "bob")
		videotest.SeedRating(t, svc.DB, other, "movie", movie.ID, 3) // not this user's

		linkOnly(t, svc, userID)
		status := setupPushAll(t, svc, userID)

		if !status.SetupDone || status.Sync != (TMDBSyncKinds{true, true, true}) || status.Pending != 3 || status.Pull != nil {
			t.Errorf("status = %+v", status)
		}
		rows := videotest.SyncRows(t, svc.DB, userID)
		if len(rows) != 3 {
			t.Fatalf("outbox = %+v, want the three rateable targets", rows)
		}
		if r := rows["episode:"+videotest.JSONNumber(epID)]; r.Op != "set" || r.Rating == nil || *r.Rating != 7 {
			t.Errorf("episode row = %+v", r)
		}
		if n := len(videotest.SyncRows(t, svc.DB, other)); n != 0 {
			t.Errorf("the other user's outbox has %d rows, want 0 — they never linked", n)
		}
	})

	t.Run("re-linking the same account keeps the setup and the switches", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, clock := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		linkOnly(t, svc, userID)
		if _, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{
			Ratings:   TMDBSetupSection{Action: TMDBSetupPush, Sync: true},
			Watchlist: TMDBSetupSection{Action: tmdbSetupSkip},
			Favorites: TMDBSetupSection{Action: tmdbSetupSkip, Sync: true},
		}); err != nil {
			t.Fatalf("SetupTMDBSync: %v", err)
		}
		if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			return markTMDBAccountBroken(ctx, tx, userID, clock.Format(time.RFC3339))
		}); err != nil {
			t.Fatal(err)
		}
		stub.Bodies["/3/authentication/session/new"] = `{"success":true,"session_id":"sess-2"}`
		linkOnly(t, svc, userID)
		st, _ := svc.TMDBLinkStatus(ctx, userID)
		if !st.Linked || st.Broken || !st.SetupDone || st.Sync != (TMDBSyncKinds{Ratings: true, Favorites: true}) || st.Pending != 1 {
			t.Errorf("status = %+v, want the setup and switches kept and the row still queued", st)
		}
		skip := TMDBSetupSection{Action: tmdbSetupSkip}
		if _, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{Ratings: skip, Watchlist: skip, Favorites: skip}); !errors.Is(err, httpx.ErrConflict) {
			t.Errorf("second setup err = %v, want conflict", err)
		}
	})

	t.Run("linking a different account resets the setup and empties the outbox", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		linkUser(t, svc, userID)
		if st, _ := svc.TMDBLinkStatus(ctx, userID); !st.SetupDone || st.Pending != 1 {
			t.Fatalf("status before = %+v", st)
		}
		stub.Bodies["/3/account"] = `{"id":43,"username":"carol"}`
		stub.Bodies["/3/authentication/session/new"] = `{"success":true,"session_id":"sess-2"}`
		linkOnly(t, svc, userID)
		st, _ := svc.TMDBLinkStatus(ctx, userID)
		if !st.Linked || st.SetupDone || st.Sync != (TMDBSyncKinds{}) || st.Pending != 0 || *st.Username != "carol" {
			t.Errorf("status = %+v, want a fresh setup for the new account", st)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("outbox rows = %d, want 0 — the old account's rows must not reach the new one", n)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name     string
			mutate   func(flow *TMDBLinkFlow, token *string, approved *bool, userID *int64, clock *time.Time)
			wantErr  error
			wantLink bool
		}{
			{name: "expired flow", mutate: func(f *TMDBLinkFlow, _ *string, _ *bool, _ *int64, clock *time.Time) {
				*clock = clock.Add(tmdbLinkFlowTTL)
			}, wantErr: httpx.ErrValidation},
			{name: "another user's flow", mutate: func(f *TMDBLinkFlow, _ *string, _ *bool, userID *int64, _ *time.Time) {
				f.UserID = *userID + 100
			}, wantErr: httpx.ErrValidation},
			{name: "token mismatch", mutate: func(_ *TMDBLinkFlow, token *string, _ *bool, _ *int64, _ *time.Time) {
				*token = "forged"
			}, wantErr: httpx.ErrValidation},
			{name: "denied on tmdb", mutate: func(_ *TMDBLinkFlow, _ *string, approved *bool, _ *int64, _ *time.Time) {
				*approved = false
			}, wantErr: errTMDBLinkDenied},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				svc, userID, clock := syncService(t, &videotest.ProviderStub{})
				_, flow, err := svc.StartTMDBLink(ctx, userID)
				if err != nil {
					t.Fatalf("StartTMDBLink: %v", err)
				}
				token, approved := flow.RequestToken, true
				tt.mutate(&flow, &token, &approved, &userID, clock)
				err = svc.CompleteTMDBLink(ctx, userID, flow, token, approved)
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if st, _ := svc.TMDBLinkStatus(ctx, userID); st.Linked {
					t.Error("linked after a refused completion")
				}
			})
		}
	})

	t.Run("tmdb refusing the token exchange is a denial", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Statuses: map[string]int{"/3/authentication/session/new": 401}}
		svc, userID, _ := syncService(t, stub)
		stub.Bodies["/3/authentication/session/new"] = `{"status_code":17}`
		_, flow, _ := svc.StartTMDBLink(ctx, userID)
		if err := svc.CompleteTMDBLink(ctx, userID, flow, flow.RequestToken, true); !errors.Is(err, errTMDBLinkDenied) {
			t.Fatalf("err = %v, want denied", err)
		}
	})

	t.Run("tmdb down at exchange is an outage and links nothing", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/authentication/session/new": videotest.ErrTimeout}}
		svc, userID, _ := syncService(t, stub)
		_, flow, _ := svc.StartTMDBLink(ctx, userID)
		if err := svc.CompleteTMDBLink(ctx, userID, flow, flow.RequestToken, true); !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v, want provider unavailable", err)
		}
	})

	t.Run("unlink empties the outbox, revokes once and is idempotent", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		linkUser(t, svc, userID)

		if err := svc.UnlinkTMDB(ctx, userID); err != nil {
			t.Fatalf("UnlinkTMDB: %v", err)
		}
		if err := svc.UnlinkTMDB(ctx, userID); err != nil {
			t.Fatalf("second UnlinkTMDB: %v", err)
		}
		st, _ := svc.TMDBLinkStatus(ctx, userID)
		if st.Linked || st.Pending != 0 {
			t.Errorf("status after unlink = %+v", st)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("outbox rows = %d, want 0", n)
		}
		if got := stub.CallCount("/3/authentication/session"); got != 1 {
			t.Errorf("session revocations = %d, want 1", got)
		}
		// The local rating is untouched: unlinking is about the push, not the opinion.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings WHERE user_id = ?`, userID); n != 1 {
			t.Errorf("ratings = %d, want 1", n)
		}
	})

	t.Run("re-linking heals a broken link and revokes the old session", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, clock := syncService(t, stub)
		linkUser(t, svc, userID)
		if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			return markTMDBAccountBroken(ctx, tx, userID, clock.Format(time.RFC3339))
		}); err != nil {
			t.Fatal(err)
		}
		if st, _ := svc.TMDBLinkStatus(ctx, userID); !st.Broken {
			t.Fatal("expected a broken link")
		}
		stub.Bodies["/3/authentication/session/new"] = `{"success":true,"session_id":"sess-2"}`
		linkUser(t, svc, userID)
		if st, _ := svc.TMDBLinkStatus(ctx, userID); !st.Linked || st.Broken {
			t.Errorf("status = %+v, want linked and not broken", st)
		}
		// The broken session is not revoked (TMDB already refused it); a
		// working one replaced by a re-link is.
		if got := stub.CallCount("/3/authentication/session"); got != 0 {
			t.Errorf("revocations = %d, want 0 for a broken session", got)
		}
		stub.Bodies["/3/authentication/session/new"] = `{"success":true,"session_id":"sess-3"}`
		linkUser(t, svc, userID)
		if got := stub.CallCount("/3/authentication/session"); got != 1 {
			t.Errorf("revocations = %d, want 1 after replacing a working session", got)
		}
	})

	t.Run("no credential: nothing to link", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		svc.TMDB = provider.NewTMDB(provider.TMDBCredentials{})
		if _, _, err := svc.StartTMDBLink(ctx, userID); !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v", err)
		}
		if st, _ := svc.TMDBLinkStatus(ctx, userID); st.Configured {
			t.Error("status reports configured with no credential")
		}
	})
}

func TestTMDBSyncEnqueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("rate writes no row before the link and one after", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		rate := func(v int) {
			if _, err := ratings.NewService(svc.Core).Rate(ctx, userID, ratings.RateParams{TrackParams: common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(movie.ID)}, Rating: videotest.Ptr(v)}); err != nil {
				t.Fatalf("Rate: %v", err)
			}
		}
		rate(6)
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Fatalf("outbox rows before link = %d, want 0", n)
		}
		linkUser(t, svc, userID) // backfills the 6
		rate(7)
		rate(9)
		rows := videotest.SyncRows(t, svc.DB, userID)
		r, ok := rows["movie:"+videotest.JSONNumber(movie.ID)]
		if len(rows) != 1 || !ok || r.Op != "set" || *r.Rating != 9 || r.Attempts != 0 {
			t.Errorf("outbox = %+v, want one row holding the latest rating", rows)
		}
	})

	t.Run("removing a rating queues a delete", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		linkUser(t, svc, userID)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		if err := ratings.NewService(svc.Core).RatingRemove(ctx, userID, "movie", videotest.JSONNumber(movie.ID)); err != nil {
			t.Fatalf("RatingRemove: %v", err)
		}
		r := videotest.SyncRows(t, svc.DB, userID)["movie:"+videotest.JSONNumber(movie.ID)]
		if r.Op != "delete" || r.Rating != nil {
			t.Errorf("row = %+v, want a delete", r)
		}
	})

	t.Run("a watch logged with a rating queues it, without one does not", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		linkUser(t, svc, userID)
		p := common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(movie.ID)}
		if _, err := watches.NewService(svc.Core).MarkWatched(ctx, userID, watchNow(p)); err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Fatalf("outbox rows after an unrated watch = %d, want 0", n)
		}
		if _, err := watches.NewService(svc.Core).MarkWatched(ctx, userID, watches.WatchParams{TrackParams: p, WatchedAt: videotest.Ptr("2026-08-01T10:00:00Z"), Rating: videotest.Ptr(8)}); err != nil {
			t.Fatalf("MarkWatched with rating: %v", err)
		}
		if r := videotest.SyncRows(t, svc.DB, userID)["movie:"+videotest.JSONNumber(movie.ID)]; r.Op != "set" || r.Rating == nil || *r.Rating != 8 {
			t.Errorf("row = %+v", r)
		}
	})

	t.Run("a restored rating queues too", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		linkUser(t, svc, userID)
		stub.Bodies["/3/movie/603"] = videotest.MatrixDetailsBody
		dump := tbDump(nil, []restore.TicketBoxRatingEntry{tbRating("movie", 603, 9, "2026-08-10T00:00:00.000Z")}, nil)
		rep := runRestore(t, svc, userID, dump)
		if rep.Report.RatingsUpserted != 1 {
			t.Fatalf("report = %+v", rep.Report)
		}
		if r := videotest.SyncRows(t, svc.DB, userID)["movie:"+videotest.JSONNumber(movie.ID)]; r.Op != "set" || r.Rating == nil || *r.Rating != 9 {
			t.Errorf("row = %+v", r)
		}
	})

	t.Run("a broken link queues nothing new", func(t *testing.T) {
		t.Parallel()
		svc, userID, clock := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		linkUser(t, svc, userID)
		if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			return markTMDBAccountBroken(ctx, tx, userID, clock.Format(time.RFC3339))
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := ratings.NewService(svc.Core).Rate(ctx, userID, ratings.RateParams{TrackParams: common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(movie.ID)}, Rating: videotest.Ptr(5)}); err != nil {
			t.Fatalf("Rate: %v", err)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("outbox rows = %d, want 0 while broken", n)
		}
	})
}

func TestTMDBSyncWatchlist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("an add or remove queues once linked, and never before", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		p := common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(movie.ID)}

		if _, err := lists.NewService(svc.Core).WatchlistAdd(ctx, userID, p); err != nil {
			t.Fatalf("WatchlistAdd: %v", err)
		}
		if err := lists.NewService(svc.Core).WatchlistRemove(ctx, userID, "movie", videotest.JSONNumber(movie.ID)); err != nil {
			t.Fatalf("WatchlistRemove: %v", err)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Fatalf("outbox rows before link = %d, want 0", n)
		}
		linkUser(t, svc, userID)

		if _, err := lists.NewService(svc.Core).WatchlistAdd(ctx, userID, p); err != nil {
			t.Fatalf("WatchlistAdd: %v", err)
		}
		if r := videotest.SyncRows(t, svc.DB, userID)["watchlist/movie:"+videotest.JSONNumber(movie.ID)]; r.Op != "set" || r.Rating != nil {
			t.Errorf("add row = %+v", r)
		}
		if err := lists.NewService(svc.Core).WatchlistRemove(ctx, userID, "movie", videotest.JSONNumber(movie.ID)); err != nil {
			t.Fatalf("WatchlistRemove: %v", err)
		}
		rows := videotest.SyncRows(t, svc.DB, userID)
		if r := rows["watchlist/movie:"+videotest.JSONNumber(movie.ID)]; len(rows) != 1 || r.Op != "delete" {
			t.Errorf("outbox = %+v, want one delete row replacing the add", rows)
		}
	})

	t.Run("linking backfills the existing watchlist beside the ratings", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		showID := videotest.SeedShow(t, svc.DB, 1399, "Thrones")
		videotest.SeedWatchlist(t, svc.DB, userID, "movie", movie.ID, "2026-08-01T00:00:00Z")
		videotest.SeedWatchlist(t, svc.DB, userID, "show", showID, "2026-08-02T00:00:00Z")
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		other := videotest.SeedUser(t, svc.DB, "bob")
		videotest.SeedWatchlist(t, svc.DB, other, "movie", movie.ID, "2026-08-03T00:00:00Z")

		linkUser(t, svc, userID)

		rows := videotest.SyncRows(t, svc.DB, userID)
		if len(rows) != 3 {
			t.Fatalf("outbox = %+v, want two watchlist rows and one rating row", rows)
		}
		for _, key := range []string{"watchlist/movie:" + videotest.JSONNumber(movie.ID), "watchlist/show:" + videotest.JSONNumber(showID)} {
			if r, ok := rows[key]; !ok || r.Op != "set" || r.Rating != nil {
				t.Errorf("%s = %+v, want a watchlist add", key, r)
			}
		}
		if st, _ := svc.TMDBLinkStatus(ctx, userID); st.Pending != 3 {
			t.Errorf("pending = %d, want 3", st.Pending)
		}
		if n := len(videotest.SyncRows(t, svc.DB, other)); n != 0 {
			t.Errorf("the other user's outbox has %d rows, want 0", n)
		}
	})

	t.Run("a restored listing queues too", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		linkUser(t, svc, userID)
		stub.Bodies["/3/movie/603"] = videotest.MatrixDetailsBody
		dump := tbDump(nil, nil, []restore.TicketBoxWatchlistEntry{tbWatchlist("movie", 603, "2026-08-10T00:00:00.000Z")})
		if rep := runRestore(t, svc, userID, dump); rep.Report.WatchlistInserted != 1 {
			t.Fatalf("report = %+v", rep.Report)
		}
		if r := videotest.SyncRows(t, svc.DB, userID)["watchlist/movie:"+videotest.JSONNumber(movie.ID)]; r.Op != "set" {
			t.Errorf("row = %+v", r)
		}
	})

	t.Run("a restored favorite queues too", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		linkUser(t, svc, userID)
		stub.Bodies["/3/movie/603"] = videotest.MatrixDetailsBody
		dump := tbFavorites(tbDump(nil, nil, nil), tbFavorite("movie", 603, "2026-08-10T00:00:00.000Z"))
		if rep := runRestore(t, svc, userID, dump); rep.Report.FavoritesInserted != 1 {
			t.Fatalf("report = %+v", rep.Report)
		}
		if r := videotest.SyncRows(t, svc.DB, userID)["favorite/movie:"+videotest.JSONNumber(movie.ID)]; r.Op != "set" || r.Rating != nil {
			t.Errorf("row = %+v", r)
		}
	})

	t.Run("a watchlist row and a rating row on the same target coexist", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		linkUser(t, svc, userID)
		p := common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(movie.ID)}
		if _, err := lists.NewService(svc.Core).WatchlistAdd(ctx, userID, p); err != nil {
			t.Fatal(err)
		}
		if _, err := ratings.NewService(svc.Core).Rate(ctx, userID, ratings.RateParams{TrackParams: p, Rating: videotest.Ptr(8)}); err != nil {
			t.Fatal(err)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 2 {
			t.Errorf("outbox rows = %d, want 2 — one per kind", n)
		}
	})

	t.Run("the worker sends add and remove to the account watchlist route", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/account/42/watchlist": `{"status_code":1}`}}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		showID := videotest.SeedShow(t, svc.DB, 1399, "Thrones")
		linkUser(t, svc, userID)
		if _, err := lists.NewService(svc.Core).WatchlistAdd(ctx, userID, common.TrackParams{TargetType: "movie", TargetID: videotest.PtrInt64(movie.ID)}); err != nil {
			t.Fatal(err)
		}
		videotest.SeedWatchlist(t, svc.DB, userID, "show", showID, "2026-08-01T00:00:00Z")
		if err := lists.NewService(svc.Core).WatchlistRemove(ctx, userID, "show", videotest.JSONNumber(showID)); err != nil {
			t.Fatal(err)
		}

		rep := svc.RunTMDBSyncBatch(ctx)
		if rep.Sent != 2 || rep.Deferred+rep.Dropped+rep.Broken != 0 {
			t.Fatalf("report = %+v", rep)
		}
		if got := stub.CallCount("/3/account/42/watchlist"); got != 2 {
			t.Errorf("watchlist calls = %d, want 2", got)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("outbox rows = %d, want 0", n)
		}
	})
}

// seedSyncTargets links the user and leaves three set rows in the outbox: a
// movie (tmdb 603), a show (tmdb 1399) and its S1E1.
func seedSyncTargets(t *testing.T, svc *Service, userID int64) (movieID, showID, epID int64) {
	t.Helper()
	movie := videotest.SeedHistoryMovie(t, svc.DB)
	showID = videotest.SeedShow(t, svc.DB, 1399, "Thrones")
	epID = videotest.SeedEpisode(t, svc.DB, showID, 1, 1, videotest.Ptr("2011-04-17"))
	videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 7)
	videotest.SeedRating(t, svc.DB, userID, "show", showID, 7)
	videotest.SeedRating(t, svc.DB, userID, "episode", epID, 7)
	linkUser(t, svc, userID)
	return movie.ID, showID, epID
}

func TestTMDBSyncBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("sends every due row to its tmdb route and deletes it", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, _ := syncService(t, stub)
		seedSyncTargets(t, svc, userID)
		for _, p := range []string{"/3/movie/603/rating", "/3/tv/1399/rating", "/3/tv/1399/season/1/episode/1/rating"} {
			stub.Bodies[p] = tmdbRatingOKBody
		}

		rep := svc.RunTMDBSyncBatch(ctx)
		if rep.Sent != 3 || rep.Deferred+rep.Dropped+rep.Broken != 0 || rep.Aborted {
			t.Fatalf("report = %+v", rep)
		}
		for _, p := range []string{"/3/movie/603/rating", "/3/tv/1399/rating", "/3/tv/1399/season/1/episode/1/rating"} {
			if stub.CallCount(p) != 1 {
				t.Errorf("%s called %d times, want 1", p, stub.CallCount(p))
			}
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("outbox rows = %d, want 0", n)
		}
		if st, _ := svc.TMDBLinkStatus(ctx, userID); st.Pending != 0 {
			t.Errorf("pending = %d", st.Pending)
		}
	})

	t.Run("a delete row calls the delete route", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603/rating": tmdbRatingOKBody}}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		linkUser(t, svc, userID)
		if err := ratings.NewService(svc.Core).RatingRemove(ctx, userID, "movie", videotest.JSONNumber(movie.ID)); err != nil {
			t.Fatal(err)
		}
		if rep := svc.RunTMDBSyncBatch(ctx); rep.Sent != 1 {
			t.Fatalf("report = %+v", rep)
		}
		if stub.CallCount("/3/movie/603/rating") != 1 {
			t.Errorf("calls = %d", stub.CallCount("/3/movie/603/rating"))
		}
	})

	t.Run("an outage defers with backoff; three in a row abort the batch", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{
			"/3/movie/603/rating":                  videotest.ErrTimeout,
			"/3/tv/1399/rating":                    videotest.ErrTimeout,
			"/3/tv/1399/season/1/episode/1/rating": videotest.ErrTimeout,
			"/3/movie/604/rating":                  videotest.ErrTimeout,
		}}
		svc, userID, clock := syncService(t, stub)
		seedSyncTargets(t, svc, userID)
		other := videotest.SeedMovie(t, svc.DB, 604, "Reloaded", nil, nil, `{}`)
		videotest.SeedRating(t, svc.DB, userID, "movie", other.ID, 5)
		if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			return common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindRating, "movie", other.ID, "set", videotest.Ptr(5), clock.Format(time.RFC3339))
		}); err != nil {
			t.Fatal(err)
		}

		rep := svc.RunTMDBSyncBatch(ctx)
		if rep.Deferred != 3 || !rep.Aborted || rep.Sent != 0 {
			t.Fatalf("report = %+v, want 3 deferred then abort", rep)
		}
		if stub.CallCount("/3/movie/604/rating") != 0 {
			t.Error("the fourth row was attempted after the breaker tripped")
		}
		rows := videotest.SyncRows(t, svc.DB, userID)
		if len(rows) != 4 {
			t.Fatalf("rows = %d, want all 4 kept", len(rows))
		}
		first := rows["movie:"+videotest.JSONNumber(seedMovieID(t, svc.DB, 603))]
		if first.Attempts != 1 || first.Retry == nil || *first.Retry != clock.Add(tmdbSyncRetryBase).Format(time.RFC3339) {
			t.Errorf("deferred row = %+v", first)
		}
		if untouched := rows["movie:"+videotest.JSONNumber(other.ID)]; untouched.Attempts != 0 || untouched.Retry != nil {
			t.Errorf("fourth row = %+v, want untouched", untouched)
		}

		// Not due yet: a batch before the retry instant sends nothing.
		if rep := svc.RunTMDBSyncBatch(ctx); rep.Sent+rep.Deferred+rep.Dropped != 1 {
			t.Errorf("report before retry = %+v, want only the fourth row attempted", rep)
		}
	})

	t.Run("a refused session marks the link broken and discards the user's rows", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{
			Statuses: map[string]int{"/3/movie/603/rating": 401},
		}
		svc, userID, _ := syncService(t, stub)
		seedSyncTargets(t, svc, userID)
		// The backfill orders rows episode, movie, show: the episode goes out,
		// the movie's 401 breaks the link, and the show is never attempted.
		stub.Bodies["/3/tv/1399/season/1/episode/1/rating"] = tmdbRatingOKBody
		stub.Bodies["/3/movie/603/rating"] = `{"status_code":3,"status_message":"Authentication failed"}`
		stub.Bodies["/3/tv/1399/rating"] = tmdbRatingOKBody

		rep := svc.RunTMDBSyncBatch(ctx)
		if rep.Sent != 1 || rep.Broken != 2 || rep.Dropped+rep.Deferred != 0 || rep.Aborted {
			t.Fatalf("report = %+v, want 1 sent then the rest counted broken", rep)
		}
		if stub.CallCount("/3/tv/1399/rating") != 0 {
			t.Error("a later row of the broken user was still sent")
		}
		st, _ := svc.TMDBLinkStatus(ctx, userID)
		if !st.Linked || !st.Broken || st.Pending != 0 {
			t.Errorf("status = %+v, want linked, broken, nothing pending", st)
		}
	})

	t.Run("a refused key is an outage, not a broken link", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Statuses: map[string]int{"/3/movie/603/rating": 401}}
		svc, userID, _ := syncService(t, stub)
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 7)
		linkUser(t, svc, userID)
		stub.Bodies["/3/movie/603/rating"] = `{"status_code":7,"status_message":"Invalid API key"}`
		if rep := svc.RunTMDBSyncBatch(ctx); rep.Deferred != 1 || rep.Broken != 0 {
			t.Fatalf("report = %+v", rep)
		}
		if st, _ := svc.TMDBLinkStatus(ctx, userID); st.Broken {
			t.Error("link marked broken on a key error")
		}
	})

	t.Run("tmdb refusing the id or the value drops the row", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Statuses: map[string]int{"/3/tv/1399/rating": 400}}
		svc, userID, _ := syncService(t, stub)
		seedSyncTargets(t, svc, userID)
		stub.Bodies["/3/tv/1399/rating"] = `{"status_code":18}`
		// /3/movie/603/rating and the episode are unscripted → 404 from the stub.
		rep := svc.RunTMDBSyncBatch(ctx)
		if rep.Dropped != 3 || rep.Deferred != 0 {
			t.Fatalf("report = %+v", rep)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("rows = %d, want 0", n)
		}
	})

	t.Run("a target whose catalog row is gone is dropped without a call", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, userID, clock := syncService(t, stub)
		linkUser(t, svc, userID)
		if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			return common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindRating, "movie", 9999, "set", videotest.Ptr(5), clock.Format(time.RFC3339))
		}); err != nil {
			t.Fatal(err)
		}
		if rep := svc.RunTMDBSyncBatch(ctx); rep.Dropped != 1 {
			t.Fatalf("report = %+v", rep)
		}
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 0 {
			t.Errorf("rows = %d", n)
		}
	})

	t.Run("a rating rewritten while in flight survives to be sent again", func(t *testing.T) {
		t.Parallel()
		svc, userID, clock := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 7)
		linkUser(t, svc, userID)
		rows, err := dueTMDBSync(ctx, svc.DB, clock.Format(time.RFC3339), tmdbSyncBatch)
		if err != nil || len(rows) != 1 {
			t.Fatalf("due = %v, %v", rows, err)
		}
		later := clock.Add(time.Minute).Format(time.RFC3339)
		if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			return common.EnqueueTMDBSync(ctx, tx, userID, common.SyncKindRating, "movie", movie.ID, "set", videotest.Ptr(9), later)
		}); err != nil {
			t.Fatal(err)
		}
		// The worker settles what it read; the guard must refuse it.
		if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			return settleTMDBSyncDone(ctx, tx, rows[0].ID, rows[0].UpdatedAt)
		}); err != nil {
			t.Fatal(err)
		}
		if r := videotest.SyncRows(t, svc.DB, userID)["movie:"+videotest.JSONNumber(movie.ID)]; r.Rating == nil || *r.Rating != 9 {
			t.Errorf("row = %+v, want the newer rating kept", r)
		}
	})

	t.Run("nothing configured, nothing sent", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		seedSyncTargets(t, svc, userID)
		svc.TMDB = provider.NewTMDB(provider.TMDBCredentials{})
		if rep := svc.RunTMDBSyncBatch(ctx); rep != (tmdbSyncReport{}) {
			t.Errorf("report = %+v", rep)
		}
	})

	t.Run("run drains a kick and stops at shutdown", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603/rating": tmdbRatingOKBody}}
		svc, userID, _ := syncService(t, stub)
		svc.tmdbSync.bootDelay = time.Hour // only a kick may wake it
		svc.tmdbSync.idlePoll = time.Hour
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 7)

		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { svc.Run(runCtx); close(done) }()

		linkUser(t, svc, userID) // backfill + kick
		deadline := time.Now().Add(5 * time.Second)
		for len(videotest.SyncRows(t, svc.DB, userID)) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("the worker never drained the kicked row")
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
	})
}

func TestTMDBSyncBackoff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		attempts int
		want     time.Duration
	}{{1, 30 * time.Second}, {2, time.Minute}, {3, 2 * time.Minute}, {8, time.Hour}, {20, time.Hour}}
	for _, tt := range tests {
		if got := tmdbSyncBackoff(tt.attempts); got != tt.want {
			t.Errorf("backoff(%d) = %v, want %v", tt.attempts, got, tt.want)
		}
	}
}

// seedMovieID reads back a seeded movie's local id by tmdb id.
func seedMovieID(t *testing.T, database *db.DB, tmdbID int64) int64 {
	t.Helper()
	var id int64
	if err := database.ReadRow(context.Background(), `SELECT id FROM movies WHERE tmdb_id = ?`, tmdbID).Scan(&id); err != nil {
		t.Fatalf("reading movie id: %v", err)
	}
	return id
}

func TestTMDBSyncSetup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	skip := TMDBSetupSection{Action: tmdbSetupSkip}

	t.Run("push queues the backfills and sets the switches", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		videotest.SeedWatchlist(t, svc.DB, userID, "movie", movie.ID, "2026-08-01T10:00:00Z")
		videotest.SeedFavorite(t, svc.DB, userID, "movie", movie.ID, "2026-08-01T10:00:00Z")
		linkOnly(t, svc, userID)

		st, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{
			Ratings:   TMDBSetupSection{Action: TMDBSetupPush, Sync: true},
			Watchlist: TMDBSetupSection{Action: TMDBSetupPush}, // one-time only
			Favorites: skip,
		})
		if err != nil {
			t.Fatalf("SetupTMDBSync: %v", err)
		}
		if !st.SetupDone || st.Sync != (TMDBSyncKinds{Ratings: true}) || st.Pending != 2 || st.Pull != nil {
			t.Errorf("status = %+v", st)
		}
		rows := videotest.SyncRows(t, svc.DB, userID)
		if _, ok := rows["movie:"+videotest.JSONNumber(movie.ID)]; !ok {
			t.Errorf("rating row missing from %+v", rows)
		}
		if _, ok := rows["watchlist/movie:"+videotest.JSONNumber(movie.ID)]; !ok {
			t.Errorf("watchlist row missing from %+v — a push with its switch off still sends once", rows)
		}
		if _, ok := rows["favorite/movie:"+videotest.JSONNumber(movie.ID)]; ok {
			t.Errorf("favorite row queued by a skip: %+v", rows)
		}
	})

	t.Run("skip everywhere queues nothing but finishes the setup", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		linkOnly(t, svc, userID)
		st, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{Ratings: skip, Watchlist: skip, Favorites: skip})
		if err != nil {
			t.Fatalf("SetupTMDBSync: %v", err)
		}
		if !st.SetupDone || st.Pending != 0 || st.Pull != nil {
			t.Errorf("status = %+v", st)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		t.Run("not linked", func(t *testing.T) {
			t.Parallel()
			svc, userID, _ := syncService(t, &videotest.ProviderStub{})
			if _, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{Ratings: skip, Watchlist: skip, Favorites: skip}); !errors.Is(err, httpx.ErrNotFound) {
				t.Errorf("err = %v, want not found", err)
			}
		})
		t.Run("unknown action", func(t *testing.T) {
			t.Parallel()
			svc, userID, _ := syncService(t, &videotest.ProviderStub{})
			linkOnly(t, svc, userID)
			if _, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{Ratings: TMDBSetupSection{Action: "merge"}, Watchlist: skip, Favorites: skip}); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("err = %v, want validation", err)
			}
		})
		t.Run("broken link", func(t *testing.T) {
			t.Parallel()
			svc, userID, clock := syncService(t, &videotest.ProviderStub{})
			linkOnly(t, svc, userID)
			if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
				return markTMDBAccountBroken(ctx, tx, userID, clock.Format(time.RFC3339))
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{Ratings: skip, Watchlist: skip, Favorites: skip}); !errors.Is(err, httpx.ErrConflict) {
				t.Errorf("err = %v, want conflict", err)
			}
		})
		t.Run("already set up", func(t *testing.T) {
			t.Parallel()
			svc, userID, _ := syncService(t, &videotest.ProviderStub{})
			linkUser(t, svc, userID)
			if _, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{Ratings: skip, Watchlist: skip, Favorites: skip}); !errors.Is(err, httpx.ErrConflict) {
				t.Errorf("err = %v, want conflict", err)
			}
		})
	})

	t.Run("a pull runs as a job and the setup finishes with it", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(map[string]string{
			"/3/account/42/rated/movies": `{"page":1,"total_pages":1,"results":[{"id":603,"rating":7.5}]}`,
			"/3/movie/603":               videotest.MatrixDetailsBody,
		})
		gate := &videotest.GatedDoer{Inner: stub, Gate: make(chan struct{})}
		svc, userID, _ := syncService(t, stub)
		linkOnly(t, svc, userID)
		svc.TMDB = videotest.NewGatedTMDB(gate) // after the link: its own calls must not wait on the gate

		st, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{
			Ratings:   TMDBSetupSection{Action: tmdbSetupPull, Sync: true},
			Watchlist: skip,
			Favorites: skip,
		})
		if err != nil {
			t.Fatalf("SetupTMDBSync: %v", err)
		}
		if st.SetupDone || st.Pull == nil || st.Pull.State != tmdbPullStateRunning || !st.Sync.Ratings {
			t.Fatalf("status = %+v, want a running pull, switches set, setup not yet done", st)
		}
		if _, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{Ratings: skip, Watchlist: skip, Favorites: skip}); !errors.Is(err, httpx.ErrConflict) {
			t.Errorf("second setup while running err = %v, want conflict", err)
		}

		close(gate.Gate)
		job := waitForPull(t, svc, userID)
		if job.State != tmdbPullStateDone || job.Report.RatingsImported != 1 || job.Report.MoviesMaterialised != 1 {
			t.Errorf("job = %+v", job)
		}
		st, _ = svc.TMDBLinkStatus(ctx, userID)
		if !st.SetupDone || st.Pull == nil || st.Pull.State != tmdbPullStateDone {
			t.Errorf("status after = %+v", st)
		}
		// The pulled rating landed, rounded, and queued nothing back.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_ratings WHERE user_id = ? AND target_type = 'movie' AND rating = 8`, userID); n != 1 {
			t.Errorf("rating rows = %d, want 1 at 8", n)
		}
		if st.Pending != 0 {
			t.Errorf("pending = %d, want 0 — a pulled write is never queued", st.Pending)
		}
	})

	t.Run("unlinking cancels a running pull and a new link does not show it", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(nil)
		// Only the account list reads wait on the gate: the unlink's own
		// session revocation and the re-link must go straight through.
		gate := &videotest.GatedDoer{Inner: stub, Gate: make(chan struct{})}
		svc, userID, clock := syncService(t, stub)
		linkOnly(t, svc, userID)
		svc.TMDB = provider.NewTMDB(provider.TMDBCredentials{APIKeyV3: "test-key"}, provider.WithHTTPDoer(videotest.PrefixGatedDoer{Gate: gate, Prefix: "/3/account/42/"}))
		if _, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{Ratings: TMDBSetupSection{Action: tmdbSetupPull}, Watchlist: skip, Favorites: skip}); err != nil {
			t.Fatalf("SetupTMDBSync: %v", err)
		}
		if err := svc.UnlinkTMDB(ctx, userID); err != nil {
			t.Fatalf("UnlinkTMDB: %v", err)
		}
		close(gate.Gate)
		if job := waitForPull(t, svc, userID); job.State != tmdbPullStateFailed || job.Error == nil || job.Error.Code != tmdbPullErrCancelled {
			t.Errorf("job = %+v, want cancelled", job)
		}
		*clock = clock.Add(time.Minute)
		linkOnly(t, svc, userID)
		if st, _ := svc.TMDBLinkStatus(ctx, userID); st.Pull != nil || st.SetupDone {
			t.Errorf("status = %+v, want no pull report from the previous link", st)
		}
	})

	t.Run("shutdown cancels a running pull and leaves the setup to be re-run", func(t *testing.T) {
		t.Parallel()
		stub := pullStub(nil)
		gate := &videotest.GatedDoer{Inner: stub, Gate: make(chan struct{})}
		svc, userID, _ := syncService(t, stub)
		svc.tmdbSync.bootDelay, svc.tmdbSync.idlePoll = time.Hour, time.Hour
		linkOnly(t, svc, userID)
		svc.TMDB = videotest.NewGatedTMDB(gate)

		runCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			svc.Run(runCtx)
			close(done)
		}()
		if _, err := svc.SetupTMDBSync(ctx, userID, TMDBSetupParams{Ratings: TMDBSetupSection{Action: tmdbSetupPull}, Watchlist: skip, Favorites: skip}); err != nil {
			t.Fatalf("SetupTMDBSync: %v", err)
		}
		stop()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Run did not return after cancel")
		}
		close(gate.Gate)
		job := waitForPull(t, svc, userID)
		if job.State != tmdbPullStateFailed || job.Error == nil || job.Error.Code != tmdbPullErrCancelled {
			t.Errorf("job = %+v, want cancelled", job)
		}
		if st, _ := svc.TMDBLinkStatus(ctx, userID); st.SetupDone {
			t.Error("setup marked done by a cancelled pull")
		}
	})
}

func TestTMDBSyncKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("the guard is per kind", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		linkOnly(t, svc, userID)
		if _, err := svc.SetTMDBSyncKinds(ctx, userID, TMDBSyncKinds{Ratings: true}); err != nil {
			t.Fatalf("SetTMDBSyncKinds: %v", err)
		}
		if _, err := ratings.NewService(svc.Core).Rate(ctx, userID, ratings.RateParams{TrackParams: common.TrackParams{TargetType: "movie", TargetID: &movie.ID}, Rating: videotest.Ptr(9)}); err != nil {
			t.Fatalf("Rate: %v", err)
		}
		if _, err := lists.NewService(svc.Core).WatchlistAdd(ctx, userID, common.TrackParams{TargetType: "movie", TargetID: &movie.ID}); err != nil {
			t.Fatalf("WatchlistAdd: %v", err)
		}
		if _, err := lists.NewService(svc.Core).FavoriteAdd(ctx, userID, common.TrackParams{TargetType: "movie", TargetID: &movie.ID}); err != nil {
			t.Fatalf("FavoriteAdd: %v", err)
		}
		rows := videotest.SyncRows(t, svc.DB, userID)
		if len(rows) != 1 {
			t.Fatalf("outbox = %+v, want the rating only", rows)
		}
		if _, ok := rows["movie:"+videotest.JSONNumber(movie.ID)]; !ok {
			t.Errorf("outbox = %+v, want the rating row", rows)
		}
	})

	t.Run("turning a kind off deletes its pending rows and only those", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		videotest.SeedWatchlist(t, svc.DB, userID, "movie", movie.ID, "2026-08-01T10:00:00Z")
		linkUser(t, svc, userID)
		if n := len(videotest.SyncRows(t, svc.DB, userID)); n != 2 {
			t.Fatalf("outbox rows = %d, want 2", n)
		}
		st, err := svc.SetTMDBSyncKinds(ctx, userID, TMDBSyncKinds{Ratings: true, Favorites: true})
		if err != nil {
			t.Fatalf("SetTMDBSyncKinds: %v", err)
		}
		rows := videotest.SyncRows(t, svc.DB, userID)
		if len(rows) != 1 || st.Pending != 1 {
			t.Fatalf("outbox = %+v, pending %d, want the rating only", rows, st.Pending)
		}
		if _, ok := rows["movie:"+videotest.JSONNumber(movie.ID)]; !ok {
			t.Errorf("outbox = %+v, want the rating row kept", rows)
		}
		if st.Sync != (TMDBSyncKinds{Ratings: true, Favorites: true}) {
			t.Errorf("sync = %+v", st.Sync)
		}
	})

	t.Run("turning a kind on queues nothing", func(t *testing.T) {
		t.Parallel()
		svc, userID, _ := syncService(t, &videotest.ProviderStub{})
		movie := videotest.SeedHistoryMovie(t, svc.DB)
		videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
		linkOnly(t, svc, userID)
		st, err := svc.SetTMDBSyncKinds(ctx, userID, TMDBSyncKinds{Ratings: true, Watchlist: true, Favorites: true})
		if err != nil {
			t.Fatalf("SetTMDBSyncKinds: %v", err)
		}
		if st.Pending != 0 {
			t.Errorf("pending = %d, want 0 — a switch never backfills", st.Pending)
		}
	})

	t.Run("not linked is a 404; broken is allowed", func(t *testing.T) {
		t.Parallel()
		svc, userID, clock := syncService(t, &videotest.ProviderStub{})
		if _, err := svc.SetTMDBSyncKinds(ctx, userID, TMDBSyncKinds{}); !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("err = %v, want not found", err)
		}
		linkOnly(t, svc, userID)
		if err := svc.DB.InTx(ctx, func(tx db.Tx) error {
			return markTMDBAccountBroken(ctx, tx, userID, clock.Format(time.RFC3339))
		}); err != nil {
			t.Fatal(err)
		}
		if st, err := svc.SetTMDBSyncKinds(ctx, userID, TMDBSyncKinds{Ratings: true}); err != nil || !st.Sync.Ratings || !st.Broken {
			t.Errorf("status = %+v, err = %v", st, err)
		}
	})
}

// pullStub scripts every account list the pull can read as an empty page,
// then layers bodies on top — so a pull of any section never hits an
// unscripted (404) list.
func pullStub(bodies map[string]string) *videotest.ProviderStub {
	const empty = `{"page":1,"total_pages":1,"results":[]}`
	stub := &videotest.ProviderStub{Bodies: map[string]string{}}
	for _, p := range []string{"rated/movies", "rated/tv", "rated/tv/episodes", "watchlist/movies", "watchlist/tv", "favorite/movies", "favorite/tv"} {
		stub.Bodies["/3/account/42/"+p] = empty
	}
	for k, v := range bodies {
		stub.Bodies[k] = v
	}
	return stub
}

// waitForPull polls the user's pull job until it stops running.
func waitForPull(t *testing.T, svc *Service, userID int64) TMDBPullJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := svc.tmdbPull.get(userID)
		if !ok {
			t.Fatal("no pull job registered")
		}
		if snap := job.snapshot(); snap.State != tmdbPullStateRunning {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("pull job did not finish")
	return TMDBPullJob{}
}
