package copies

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/details"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// seedSpecialsSeason gives the tracked fixture show a Specials row, which the
// TMDB fixture body deliberately lacks — the fan-out tests need one to prove
// season 0 never rides a show-level action.
func seedSpecialsSeason(t *testing.T, svc *Service, show common.ShowRow) common.SeasonRow {
	t.Helper()
	var row common.SeasonRow
	err := svc.DB.InTx(context.Background(), func(tx db.Tx) error {
		var txErr error
		row, txErr = common.UpsertSeasonByTMDBKey(context.Background(), tx, show.ID, common.SeasonRow{
			ShowTMDBID:   show.TMDBID,
			SeasonNumber: 0,
			Name:         "Specials",
			ShowTitle:    show.Title,
			MetadataJSON: "{}",
			FetchedAt:    "2026-08-19T12:00:00Z",
		})
		return txErr
	})
	if err != nil {
		t.Fatalf("seeding specials season: %v", err)
	}
	return row
}

func TestCopyAdd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a movie copy by tmdb_id materialises the movie and is idempotent", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.CopyAdd(ctx, userID, CopyParams{
			TrackParams: common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}, Kind: "dvd",
		})
		if err != nil {
			t.Fatalf("CopyAdd: %v", err)
		}
		if res.TargetType != "movie" || res.TargetID < 1 || res.Kind != "dvd" || res.Created != 1 {
			t.Fatalf("result = %+v", res)
		}
		// A copy is a tracking action (D-015): the catalog row now exists.
		if _, err := common.GetMovieByTMDBID(ctx, svc.DB, 603); err != nil {
			t.Fatalf("adding a copy did not materialise the movie: %v", err)
		}
		// And a reference (D-008): the copy alone keeps the movie alive.
		referenced, err := common.MovieReferenced(ctx, svc.DB, res.TargetID)
		if err != nil {
			t.Fatalf("movieReferenced: %v", err)
		}
		if !referenced {
			t.Error("a copy did not count as a reference")
		}

		again, err := svc.CopyAdd(ctx, userID, CopyParams{
			TrackParams: common.TrackParams{TargetType: "movie", TargetID: &res.TargetID}, Kind: "dvd",
		})
		if err != nil {
			t.Fatalf("second CopyAdd: %v", err)
		}
		if again.Created != 0 {
			t.Errorf("repeat add created = %d, want 0", again.Created)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_copies`); n != 1 {
			t.Errorf("copy rows = %d, want 1", n)
		}
	})

	t.Run("a TMDB outage fails the add and writes nothing", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Errs: map[string]error{"/3/movie/603": errors.New("dial tcp: connection refused")}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		_, err := svc.CopyAdd(ctx, userID, CopyParams{
			TrackParams: common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}, Kind: "dvd",
		})
		if !errors.Is(err, httpx.ErrProviderUnavailable) {
			t.Fatalf("err = %v, want ErrProviderUnavailable", err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_copies`); n != 0 {
			t.Errorf("copy rows = %d, want 0 — a failed materialise must write nothing", n)
		}
	})

	t.Run("a season copy via the tmdb pair materialises the whole show", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		res, err := svc.CopyAdd(ctx, userID, CopyParams{
			TrackParams: common.TrackParams{TargetType: "season", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(2)},
			Kind:        "bluray",
		})
		if err != nil {
			t.Fatalf("CopyAdd: %v", err)
		}
		if res.TargetType != "season" || res.Created != 1 {
			t.Fatalf("result = %+v", res)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM episodes`); n != 5 {
			t.Errorf("episodes = %d, want 5 — a season copy must pull the whole show local", n)
		}
	})

	t.Run("a season is addressable by its local id once tracked, Specials included", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		specials := seedSpecialsSeason(t, svc, show)

		res, err := svc.CopyAdd(ctx, userID, CopyParams{
			TrackParams: common.TrackParams{TargetType: "season", TargetID: &specials.ID}, Kind: "dvd",
		})
		if err != nil {
			t.Fatalf("CopyAdd: %v", err)
		}
		if res.TargetID != specials.ID || res.Created != 1 {
			t.Fatalf("result = %+v", res)
		}
	})

	t.Run("a show scope fans out to every non-special season", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		seedSpecialsSeason(t, svc, show)

		res, err := svc.CopyAdd(ctx, userID, CopyParams{
			TrackParams: common.TrackParams{TargetType: "show", TargetID: &show.ID}, Kind: "jellyfin",
		})
		if err != nil {
			t.Fatalf("CopyAdd: %v", err)
		}
		// The fixture show has seasons 1 and 2; the seeded Specials must not ride.
		if res.TargetType != "show" || res.TargetID != show.ID || res.Created != 2 {
			t.Fatalf("result = %+v, want created 2", res)
		}
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_video_copies c
			 JOIN seasons s ON s.id = c.target_id
			 WHERE c.target_type = 'season' AND s.season_number = 0`); n != 0 {
			t.Errorf("specials copy rows = %d, want 0 — season 0 never rides a show-level add", n)
		}
		// No show-target row either: 'show' is a scope, never a stored target.
		if n := videotest.CountRows(t, svc.DB,
			`SELECT COUNT(*) FROM user_video_copies WHERE target_type = 'show'`); n != 0 {
			t.Errorf("show-target copy rows = %d, want 0", n)
		}

		// A partial pre-existing kind: only the missing seasons gain rows.
		season1, err := common.GetSeasonByTMDBKey(ctx, svc.DB, show.TMDBID, 1)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		videotest.SeedCopy(t, svc.DB, userID, "season", season1.ID, "dvd")
		partial, err := svc.CopyAdd(ctx, userID, CopyParams{
			TrackParams: common.TrackParams{TargetType: "show", TargetID: &show.ID}, Kind: "dvd",
		})
		if err != nil {
			t.Fatalf("partial CopyAdd: %v", err)
		}
		if partial.Created != 1 {
			t.Errorf("partial add created = %d, want 1", partial.Created)
		}
	})

	t.Run("bad params", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		tests := []struct {
			name string
			p    CopyParams
		}{
			{"unknown kind", CopyParams{
				TrackParams: common.TrackParams{TargetType: "show", TargetID: &show.ID}, Kind: "betamax"}},
			{"empty kind", CopyParams{
				TrackParams: common.TrackParams{TargetType: "show", TargetID: &show.ID}}},
			{"episode target", CopyParams{
				TrackParams: common.TrackParams{TargetType: "episode", TargetID: videotest.PtrInt64(1)}, Kind: "dvd"}},
			{"season field on a show target", CopyParams{
				TrackParams: common.TrackParams{TargetType: "show", TargetID: &show.ID, Season: videotest.PtrInt(1)}, Kind: "dvd"}},
			{"both addresses", CopyParams{
				TrackParams: common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603), TargetID: videotest.PtrInt64(1)}, Kind: "dvd"}},
		}
		for _, tc := range tests {
			t.Run(tc.name+" is a 422", func(t *testing.T) {
				if _, err := svc.CopyAdd(ctx, userID, tc.p); !errors.Is(err, httpx.ErrValidation) {
					t.Errorf("err = %v, want ErrValidation", err)
				}
			})
		}
	})
}

func TestCopyRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a show scope removes the kind from non-special seasons only", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		specials := seedSpecialsSeason(t, svc, show)
		otherUser := videotest.SeedUser(t, svc.DB, "bob")

		if _, err := svc.CopyAdd(ctx, userID, CopyParams{
			TrackParams: common.TrackParams{TargetType: "show", TargetID: &show.ID}, Kind: "dvd",
		}); err != nil {
			t.Fatalf("CopyAdd: %v", err)
		}
		// What the fan-out remove must spare: the same kind on Specials, another
		// kind on a covered season, and another user's rows for the same kind.
		videotest.SeedCopy(t, svc.DB, userID, "season", specials.ID, "dvd")
		season1, err := common.GetSeasonByTMDBKey(ctx, svc.DB, show.TMDBID, 1)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		videotest.SeedCopy(t, svc.DB, userID, "season", season1.ID, "uhd")
		videotest.SeedCopy(t, svc.DB, otherUser, "season", season1.ID, "dvd")

		if err := svc.CopyRemove(ctx, userID, "show", strconv.FormatInt(show.ID, 10), "dvd"); err != nil {
			t.Fatalf("CopyRemove: %v", err)
		}
		remaining := func(query string, args ...any) int { return videotest.CountRows(t, svc.DB, query, args...) }
		if n := remaining(`SELECT COUNT(*) FROM user_video_copies
			WHERE user_id = ? AND kind = 'dvd'`, userID); n != 1 {
			t.Errorf("this user's dvd rows = %d, want 1 (the Specials copy)", n)
		}
		if n := remaining(`SELECT COUNT(*) FROM user_video_copies
			WHERE user_id = ? AND kind = 'uhd'`, userID); n != 1 {
			t.Errorf("other kinds lost rows: uhd = %d, want 1", n)
		}
		if n := remaining(`SELECT COUNT(*) FROM user_video_copies
			WHERE user_id = ?`, otherUser); n != 1 {
			t.Errorf("another user's rows = %d, want 1 — deletes must be user-scoped", n)
		}
	})

	t.Run("removing an absent copy is idempotent, and validation still applies", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		showID := strconv.FormatInt(show.ID, 10)
		if err := svc.CopyRemove(ctx, userID, "show", showID, "uhd"); err != nil {
			t.Errorf("removing what is not held errored: %v", err)
		}
		if err := svc.CopyRemove(ctx, userID, "show", showID, "betamax"); !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("bad kind err = %v, want ErrValidation", err)
		}
		if err := svc.CopyRemove(ctx, userID, "episode", "1", "dvd"); !errors.Is(err, httpx.ErrValidation) {
			t.Errorf("episode target err = %v, want ErrValidation", err)
		}
	})
}

func TestCopiesInDetails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("movie details carry this user's copies, and only this user's", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		bob := videotest.SeedUser(t, svc.DB, "bob")

		// A live untracked view carries no user state: copies is [].
		live, err := details.NewService(svc.Core).MovieDetails(ctx, alice, details.MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if live.Copies == nil || len(live.Copies) != 0 {
			t.Errorf("live copies = %#v, want []", live.Copies)
		}

		res, err := svc.CopyAdd(ctx, alice, CopyParams{
			TrackParams: common.TrackParams{TargetType: "movie", TMDBID: videotest.PtrInt64(603)}, Kind: "uhd",
		})
		if err != nil {
			t.Fatalf("CopyAdd: %v", err)
		}
		videotest.SeedCopy(t, svc.DB, alice, "movie", res.TargetID, "jellyfin")
		d, err := details.NewService(svc.Core).MovieDetails(ctx, alice, details.MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if !slices.Equal(d.Copies, []string{"jellyfin", "uhd"}) {
			t.Errorf("copies = %v, want [jellyfin uhd] (kind-ordered)", d.Copies)
		}
		bobView, err := details.NewService(svc.Core).MovieDetails(ctx, bob, details.MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails as bob: %v", err)
		}
		if len(bobView.Copies) != 0 {
			t.Errorf("bob sees alice's copies: %v", bobView.Copies)
		}
	})

	t.Run("a copy on movie N does not surface on season N (D-013)", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		season1, err := common.GetSeasonByTMDBKey(ctx, svc.DB, show.TMDBID, 1)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		// Same id, wrong table: a movie-target row must not read as a season's.
		videotest.SeedCopy(t, svc.DB, userID, "movie", season1.ID, "dvd")
		kinds, err := common.UserTargetCopies(ctx, svc.DB, userID, "season", season1.ID)
		if err != nil {
			t.Fatalf("userTargetCopies: %v", err)
		}
		if len(kinds) != 0 {
			t.Errorf("season copies = %v, want [] — the pair identifies the target", kinds)
		}
	})

	t.Run("show details carry per-season copies", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		season2, err := common.GetSeasonByTMDBKey(ctx, svc.DB, show.TMDBID, 2)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		videotest.SeedCopy(t, svc.DB, userID, "season", season2.ID, "dvd")

		d, err := details.NewService(svc.Core).ShowDetails(ctx, userID, details.ShowDetailsParams{TMDBID: "1396"})
		if err != nil {
			t.Fatalf("ShowDetails: %v", err)
		}
		if len(d.Seasons) != 2 {
			t.Fatalf("seasons = %d, want 2", len(d.Seasons))
		}
		if len(d.Seasons[0].Copies) != 0 {
			t.Errorf("season 1 copies = %v, want []", d.Seasons[0].Copies)
		}
		if !slices.Equal(d.Seasons[1].Copies, []string{"dvd"}) {
			t.Errorf("season 2 copies = %v, want [dvd]", d.Seasons[1].Copies)
		}
	})

	t.Run("season details carry copies, and a live view carries none", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, _ := detailsService(t, stub)
		userID := videotest.SeedUser(t, svc.DB, "alice")

		live, err := details.NewService(svc.Core).SeasonDetails(ctx, userID, details.SeasonDetailsParams{TMDBID: "1396", Season: "2"})
		if err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		if live.ID != nil || live.Copies == nil || len(live.Copies) != 0 {
			t.Errorf("live season id = %v copies = %#v, want null and []", live.ID, live.Copies)
		}

		res, err := svc.CopyAdd(ctx, userID, CopyParams{
			TrackParams: common.TrackParams{TargetType: "season", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(2)},
			Kind:        "physical",
		})
		if err != nil {
			t.Fatalf("CopyAdd: %v", err)
		}
		d, err := details.NewService(svc.Core).SeasonDetails(ctx, userID, details.SeasonDetailsParams{TMDBID: "1396", Season: "2"})
		if err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		if d.ID == nil || *d.ID != res.TargetID {
			t.Fatalf("season id = %v, want %d", d.ID, res.TargetID)
		}
		if !slices.Equal(d.Copies, []string{"physical"}) {
			t.Errorf("copies = %v, want [physical]", d.Copies)
		}
	})

	// An episode is never itself owned (D-021), so its page borrows its
	// SEASON's copies — and must not pick up a sibling season's.
	t.Run("episode details carry the owning season's copies", func(t *testing.T) {
		t.Parallel()
		svc, userID, show := trackedShowService(t)
		season2, err := common.GetSeasonByTMDBKey(ctx, svc.DB, show.TMDBID, 2)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		season1, err := common.GetSeasonByTMDBKey(ctx, svc.DB, show.TMDBID, 1)
		if err != nil {
			t.Fatalf("getSeasonByTMDBKey: %v", err)
		}
		videotest.SeedCopy(t, svc.DB, userID, "season", season2.ID, "dvd")
		videotest.SeedCopy(t, svc.DB, userID, "season", season1.ID, "uhd")

		d, err := details.NewService(svc.Core).EpisodeDetails(ctx, userID, details.EpisodeDetailsParams{
			TMDBID: "1396", Season: "2", Episode: "1",
		})
		if err != nil {
			t.Fatalf("EpisodeDetails: %v", err)
		}
		if !slices.Equal(d.SeasonCopies, []string{"dvd"}) {
			t.Errorf("season_copies = %v, want [dvd] — season 1's uhd must not leak in", d.SeasonCopies)
		}
	})
}
