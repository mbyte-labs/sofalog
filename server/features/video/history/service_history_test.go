package history

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/features/video/watches"
	"mbyte.ro/sofalog/httpx"
)

func historyIDs(entries []HistoryEntry) []int64 {
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

// historyFixture is a movies-only feed carrying every tripwire that is easy to
// get wrong, so the assertions below can be read as one story:
//   - w3 and w4 share an EXACT watched_at (one bulk mark writes one instant to
//     many rows): a grouping bug renders a duplicate month bar, and ordering has
//     to fall back to the row id because the timestamp cannot break the tie;
//   - w3 is a rewatch of movieA, already watched in w1, so a list keyed on the
//     target id instead of the watch-row id fails immediately, and both entries
//     must carry the SAME ratings — a rating belongs to the target, and there is
//     no rating history;
//   - movieB has no poster and no community score and is unrated, exercising all
//     three absent branches, and proves two movies on one page stay distinct;
//   - an episode watch carries movieA's id, so anything predicating on a bare
//     target id renders a visibly wrong row (D-013);
//   - bob watched movieA too, and must be invisible;
//   - one watch points at a movie that is not in the catalog — target_id has no
//     foreign key, so a dangling id is representable and must be skipped.
type historyFixture struct {
	svc            *Service
	alice, bob     int64
	movieA, movieB common.MovieRow
	w1, w2, w3, w4 int64
	danglingID     int64
}

func newHistoryFixture(t *testing.T) historyFixture {
	t.Helper()
	f := historyFixture{svc: &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}}
	f.movieA = videotest.SeedHistoryMovie(t, f.svc.DB)
	f.movieB = videotest.SeedMovie(t, f.svc.DB, 604, "The Quiet Ledger", nil, nil,
		`{"external_ids": {}, "images": {}}`)
	f.alice = videotest.SeedUser(t, f.svc.DB, "alice")
	f.bob = videotest.SeedUser(t, f.svc.DB, "bob")

	f.w1 = videotest.SeedWatch(t, f.svc.DB, f.alice, "movie", f.movieA.ID, "2026-06-02T20:15:00Z")
	f.w2 = videotest.SeedWatch(t, f.svc.DB, f.alice, "movie", f.movieB.ID, "2026-07-28T19:40:00Z")
	f.w3 = videotest.SeedWatch(t, f.svc.DB, f.alice, "movie", f.movieA.ID, "2026-08-15T21:05:00Z")
	f.w4 = videotest.SeedWatch(t, f.svc.DB, f.alice, "movie", f.movieB.ID, "2026-08-15T21:05:00Z")
	videotest.SeedRating(t, f.svc.DB, f.alice, "movie", f.movieA.ID, 8)

	videotest.SeedWatch(t, f.svc.DB, f.alice, "episode", f.movieA.ID, "2026-07-01T20:00:00Z")
	videotest.SeedWatch(t, f.svc.DB, f.bob, "movie", f.movieA.ID, "2026-07-02T20:00:00Z")
	f.danglingID = videotest.SeedWatch(t, f.svc.DB, f.alice, "movie", 999999, "2026-07-03T20:00:00Z")
	return f
}

func (f historyFixture) history(t *testing.T, p HistoryParams) []HistoryEntry {
	t.Helper()
	entries, err := f.svc.History(context.Background(), f.alice, p)
	if err != nil {
		t.Fatalf("History(%+v) error = %v", p, err)
	}
	return entries
}

func wantIDs(t *testing.T, got, want []int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

func TestHistoryOrderingAndFiltering(t *testing.T) {
	t.Parallel()
	f := newHistoryFixture(t)

	tests := []struct {
		name    string
		params  HistoryParams
		wantIDs []int64
	}{
		{
			// Default: newest first, id descending within the same instant.
			name:    "defaults to watched_at then id descending",
			params:  HistoryParams{},
			wantIDs: []int64{f.w4, f.w3, f.w2, f.w1},
		},
		{
			name:    "ascending reverses both keys",
			params:  HistoryParams{Order: "asc"},
			wantIDs: []int64{f.w1, f.w2, f.w3, f.w4},
		},
		{
			// M1 logs only movie watches, so type=movie is the whole feed.
			name:    "type=movie keeps only movie watches",
			params:  HistoryParams{Type: "movie"},
			wantIDs: []int64{f.w4, f.w3, f.w2, f.w1},
		},
		{
			name:    "type=all is the same as omitting it",
			params:  HistoryParams{Type: "all"},
			wantIDs: []int64{f.w4, f.w3, f.w2, f.w1},
		},
		{
			name:    "limit takes the first page after ordering",
			params:  HistoryParams{Limit: "2"},
			wantIDs: []int64{f.w4, f.w3},
		},
		{
			name:    "offset skips into the ordered feed",
			params:  HistoryParams{Limit: "2", Offset: "2"},
			wantIDs: []int64{f.w2, f.w1},
		},
		{
			name:    "offset alone drops the head",
			params:  HistoryParams{Offset: "3"},
			wantIDs: []int64{f.w1},
		},
		{
			name:    "offset past the end is an empty page, not an error",
			params:  HistoryParams{Limit: "20", Offset: "40"},
			wantIDs: []int64{},
		},
		{
			name:    "limit larger than the feed returns everything",
			params:  HistoryParams{Limit: "50"},
			wantIDs: []int64{f.w4, f.w3, f.w2, f.w1},
		},
		{
			// Every watch of one movie: the rewatch pair, and the id-colliding
			// episode watch excluded because the predicate carries target_type.
			name:    "movie target returns that movie's watches only",
			params:  HistoryParams{TargetType: "movie", TargetID: fmt.Sprint(f.movieA.ID)},
			wantIDs: []int64{f.w3, f.w1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wantIDs(t, historyIDs(f.history(t, tt.params)), tt.wantIDs)
		})
	}
}

// The episode arm of the payload stays in the contract but has nothing to
// project against until shows land: no episodes catalog table exists, so an
// episode-shaped request is an empty page rather than a validation error.
func TestHistoryEpisodeArmIsEmpty(t *testing.T) {
	t.Parallel()
	f := newHistoryFixture(t)

	// The seeded episode watch really is in the log — this proves exclusion,
	// not an empty table.
	var episodeWatches int
	if err := f.svc.DB.ReadRow(context.Background(),
		`SELECT COUNT(*) FROM user_watch_history WHERE user_id = ? AND target_type = 'episode'`,
		f.alice).Scan(&episodeWatches); err != nil {
		t.Fatalf("counting episode watches: %v", err)
	}
	if episodeWatches == 0 {
		t.Fatal("fixture lost its episode watch row")
	}

	for _, p := range []HistoryParams{
		{Type: "episode"},
		{TargetType: "episode", TargetID: fmt.Sprint(f.movieA.ID)},
	} {
		entries := f.history(t, p)
		if entries == nil || len(entries) != 0 {
			t.Errorf("History(%+v) = %v, want an empty (non-null) array", p, entries)
		}
	}
}

// A watch whose catalog row is gone is skipped, and skipped in SQL — so it
// never eats a slot out of the requested page either.
func TestHistoryOmitsWatchWithNoCatalogRow(t *testing.T) {
	t.Parallel()
	f := newHistoryFixture(t)

	for _, e := range f.history(t, HistoryParams{}) {
		if e.ID == f.danglingID {
			t.Fatalf("entry %d has no catalog row and must not appear", e.ID)
		}
	}
	wantIDs(t, historyIDs(f.history(t, HistoryParams{Limit: "4"})),
		[]int64{f.w4, f.w3, f.w2, f.w1})
}

func TestHistoryIsPerUser(t *testing.T) {
	t.Parallel()
	f := newHistoryFixture(t)

	bobs, err := f.svc.History(context.Background(), f.bob, HistoryParams{})
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(bobs) != 1 {
		t.Fatalf("bob's feed = %v, want his single watch", historyIDs(bobs))
	}
	for _, id := range historyIDs(f.history(t, HistoryParams{})) {
		if id == bobs[0].ID {
			t.Fatalf("bob's watch %d leaked into alice's feed", id)
		}
	}
	// Bob rated nothing, so alice's rating of the same movie must not show up
	// on his entry.
	if bobs[0].UserRating != nil {
		t.Errorf("bob's user_rating = %v, want null — alice's rating is not his", *bobs[0].UserRating)
	}
}

// Two movies interleaved on one page: a swapped snapshot is invisible with a
// single-movie fixture.
func TestHistoryProjectsEachMovieFromItsOwnSnapshot(t *testing.T) {
	t.Parallel()
	f := newHistoryFixture(t)

	for _, e := range f.history(t, HistoryParams{}) {
		if e.Movie == nil {
			t.Fatalf("entry %d has no movie", e.ID)
		}
		switch e.Movie.ID {
		case f.movieA.ID:
			if e.Movie.Title != "The Matrix" || e.Movie.ReleaseDate == nil || *e.Movie.ReleaseDate != "1999-03-30" {
				t.Errorf("entry %d movie = %+v", e.ID, e.Movie)
			}
			if e.Poster == nil || *e.Poster != "api/v1/video/posters/w185/m.jpg" {
				t.Errorf("entry %d poster = %v", e.ID, e.Poster)
			}
			if e.PosterFull == nil || *e.PosterFull != "api/v1/video/posters/w500/m.jpg" {
				t.Errorf("entry %d poster_full = %v", e.ID, e.PosterFull)
			}
			if e.Rating == nil || *e.Rating != 8.2 {
				t.Errorf("entry %d rating = %v, want the snapshot's 8.2", e.ID, e.Rating)
			}
			if e.UserRating == nil || *e.UserRating != 8 {
				t.Errorf("entry %d user_rating = %v, want 8 on every entry of the target", e.ID, e.UserRating)
			}
		case f.movieB.ID:
			if e.Movie.Title != "The Quiet Ledger" || e.Movie.ReleaseDate != nil {
				t.Errorf("entry %d movie = %+v", e.ID, e.Movie)
			}
			if e.Poster != nil || e.PosterFull != nil || e.Rating != nil || e.UserRating != nil {
				t.Errorf("entry %d = poster %v, poster_full %v, rating %v, user_rating %v; all must be null",
					e.ID, e.Poster, e.PosterFull, e.Rating, e.UserRating)
			}
		default:
			t.Errorf("entry %d targets unknown movie %d", e.ID, e.Movie.ID)
		}
	}
}

// The SPA groups the feed into month bars with a single pass over adjacent
// entries, which is only correct if same-month entries are contiguous.
func TestHistoryGroupsSameMonthContiguously(t *testing.T) {
	t.Parallel()
	f := newHistoryFixture(t)

	for _, order := range []string{"asc", "desc"} {
		t.Run(order, func(t *testing.T) {
			t.Parallel()
			entries := f.history(t, HistoryParams{Order: order})
			seen := map[string]bool{}
			prev := ""
			for _, e := range entries {
				month := e.WatchedAt[:7]
				if month == prev {
					continue
				}
				if seen[month] {
					t.Fatalf("month %s reappears after %s: %v", month, prev, historyIDs(entries))
				}
				seen[month] = true
				prev = month
			}
			if len(seen) < 3 {
				t.Fatalf("fixture spans %d months, want at least 3 to be worth checking", len(seen))
			}
		})
	}
}

func TestHistoryEntryInvariants(t *testing.T) {
	t.Parallel()
	f := newHistoryFixture(t)
	entries := f.history(t, HistoryParams{})

	ids := map[int64]bool{}
	// A rating belongs to the target, not the watch: two entries for one target
	// must agree, because re-rating overwrites and there is no rating history.
	ratingByTarget := map[string]*int{}

	for _, e := range entries {
		if ids[e.ID] {
			t.Errorf("duplicate entry id %d — the SPA keys its list on it", e.ID)
		}
		ids[e.ID] = true

		// Exactly one side of the union, agreeing with TargetType.
		var targetID int64
		switch e.TargetType {
		case "movie":
			if e.Movie == nil || e.Episode != nil {
				t.Errorf("entry %d: target_type=movie but movie=%v episode=%v", e.ID, e.Movie, e.Episode)
				continue
			}
			targetID = e.Movie.ID
		default:
			t.Errorf("entry %d: target_type = %q, want movie — M1 logs no episode watches", e.ID, e.TargetType)
			continue
		}

		if e.WatchType != "watch" {
			t.Errorf("entry %d: watch_type = %q; M1 records only 'watch'", e.ID, e.WatchType)
		}
		if e.UserRating != nil && (*e.UserRating < 1 || *e.UserRating > 10) {
			t.Errorf("entry %d: user_rating = %d, want 1–10", e.ID, *e.UserRating)
		}

		// The key carries target_type: movie 5 and episode 5 both exist (D-013).
		key := e.TargetType + ":" + strconv.FormatInt(targetID, 10)
		if prev, ok := ratingByTarget[key]; ok {
			switch {
			case (prev == nil) != (e.UserRating == nil):
				t.Errorf("target %s: one entry rated, another not — a rating is per target", key)
			case prev != nil && *prev != *e.UserRating:
				t.Errorf("target %s: ratings %d and %d differ — no rating history exists", key, *prev, *e.UserRating)
			}
		}
		ratingByTarget[key] = e.UserRating
	}

	// The fixture must keep exercising the rewatch tripwire.
	if !ids[f.w1] || !ids[f.w3] {
		t.Error("fixture lost the rewatch pair")
	}
}

func TestHistoryInvalidParams(t *testing.T) {
	t.Parallel()
	// A real db, so a future reordering of validation and query cannot turn a
	// 422 case into a nil-pointer panic instead.
	svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}

	tests := []struct {
		name   string
		params HistoryParams
	}{
		{"invalid order", HistoryParams{Order: "sideways"}},
		{"invalid type", HistoryParams{Type: "banana"}},
		{"show is not a history target", HistoryParams{Type: "show"}},
		{"target_type without target_id", HistoryParams{TargetType: "movie"}},
		{"target_id without target_type", HistoryParams{TargetID: "5"}},
		{"invalid target_type", HistoryParams{TargetType: "show", TargetID: "5"}},
		{"non-integer target_id", HistoryParams{TargetType: "movie", TargetID: "abc"}},
		{"non-positive target_id", HistoryParams{TargetType: "movie", TargetID: "0"}},
		{"non-integer limit", HistoryParams{Limit: "abc"}},
		{"zero limit", HistoryParams{Limit: "0"}},
		{"negative offset", HistoryParams{Offset: "-1"}},
		{"non-integer offset", HistoryParams{Offset: "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := svc.History(context.Background(), 1, tt.params); !errors.Is(err, httpx.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestHistoryMovieTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
	movie := videotest.SeedHistoryMovie(t, svc.DB)
	userID := videotest.SeedUser(t, svc.DB, "alice")
	target := func(p HistoryParams) HistoryParams {
		p.TargetType = "movie"
		p.TargetID = fmt.Sprint(movie.ID)
		return p
	}

	// A first watch and a rewatch — two real log rows for one target (D-009) —
	// plus a rating, which belongs to the target and must appear on both.
	w1 := videotest.SeedWatch(t, svc.DB, userID, "movie", movie.ID, "2026-05-01T20:00:00Z")
	w2 := videotest.SeedWatch(t, svc.DB, userID, "movie", movie.ID, "2026-08-10T21:30:00Z")
	videotest.SeedRating(t, svc.DB, userID, "movie", movie.ID, 8)
	// An id-colliding episode watch: episode <movie.ID> must never leak into
	// movie <movie.ID>'s feed (D-013). The bare id needs no episodes row —
	// polymorphic columns have no FK.
	videotest.SeedWatch(t, svc.DB, userID, "episode", movie.ID, "2026-06-01T20:00:00Z")
	// Another user's watch of the same movie stays invisible.
	otherID := videotest.SeedUser(t, svc.DB, "bob")
	videotest.SeedWatch(t, svc.DB, otherID, "movie", movie.ID, "2026-07-01T20:00:00Z")

	all, err := svc.History(ctx, userID, target(HistoryParams{}))
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if got := historyIDs(all); len(got) != 2 || got[0] != w2 || got[1] != w1 {
		t.Fatalf("ids = %v, want [%d %d] (desc)", got, w2, w1)
	}
	for i, e := range all {
		if e.TargetType != "movie" || e.Movie == nil || e.Movie.ID != movie.ID {
			t.Fatalf("entry %d targets %s/%v, want this movie", i, e.TargetType, e.Movie)
		}
		if e.Movie.Title != "The Matrix" || e.Movie.ReleaseDate == nil || *e.Movie.ReleaseDate != "1999-03-30" {
			t.Errorf("entry %d movie = %+v", i, e.Movie)
		}
		if e.WatchType != "watch" {
			t.Errorf("entry %d: watch_type = %q", i, e.WatchType)
		}
		// Poster and community rating come from the stored snapshot; the user
		// rating belongs to the target, so both entries carry the same 8.
		if e.Poster == nil || *e.Poster != "api/v1/video/posters/w185/m.jpg" {
			t.Errorf("entry %d: poster = %v", i, e.Poster)
		}
		if e.Rating == nil || *e.Rating != 8.2 {
			t.Errorf("entry %d: rating = %v", i, e.Rating)
		}
		if e.UserRating == nil || *e.UserRating != 8 {
			t.Errorf("entry %d: user_rating = %v, want 8 on every entry of the target", i, e.UserRating)
		}
	}

	// Ascending order reverses the feed; paging slices it.
	asc, err := svc.History(ctx, userID, target(HistoryParams{Order: "asc"}))
	if err != nil {
		t.Fatalf("History(asc) error = %v", err)
	}
	if got := historyIDs(asc); got[0] != w1 || got[1] != w2 {
		t.Fatalf("asc ids = %v, want [%d %d]", got, w1, w2)
	}
	page, err := svc.History(ctx, userID, target(HistoryParams{Limit: "1", Offset: "1"}))
	if err != nil {
		t.Fatalf("History(paged) error = %v", err)
	}
	if got := historyIDs(page); len(got) != 1 || got[0] != w1 {
		t.Fatalf("page ids = %v, want [%d]", got, w1)
	}

	// The type filter composes: a movie target is invisible to type=episode.
	none, err := svc.History(ctx, userID, target(HistoryParams{Type: "episode"}))
	if err != nil {
		t.Fatalf("History(type=episode) error = %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("len = %d, want 0", len(none))
	}
}

func TestHistoryMovieTargetUnknownMovie(t *testing.T) {
	t.Parallel()

	svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
	entries, err := svc.History(context.Background(), 1,
		HistoryParams{TargetType: "movie", TargetID: "999"})
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if entries == nil || len(entries) != 0 {
		t.Fatalf("entries = %v, want an empty (non-null) array", entries)
	}
}

// The episode arm carries the season's local id, because that plus the episode
// number is the only way the episode details page can be addressed — an episode
// has no id-addressed endpoint, and the feed must never hand the client a TMDB
// id to navigate by.
func TestHistoryEpisodeCarriesSeasonID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, userID, show := trackedShowService(t)

	if _, err := watches.NewService(svc.Core).MarkWatched(ctx, userID, watchNow(common.TrackParams{
		TargetType: "episode", TMDBID: videotest.PtrInt64(1396), Season: videotest.PtrInt(1), Episode: videotest.PtrInt(1),
	})); err != nil {
		t.Fatalf("MarkWatched: %v", err)
	}

	entries, err := svc.History(ctx, userID, HistoryParams{Type: "episode"})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(entries) != 1 || entries[0].Episode == nil {
		t.Fatalf("entries = %+v, want one episode entry", entries)
	}
	got := entries[0].Episode
	if got.SeasonID == nil {
		t.Fatal("season_id = null, want the local seasons.id of season 1")
	}

	// It must be THAT season's row, not merely some season: a wrong id here
	// routes the user to another season's episode of the same number.
	want := videotest.CountRows(t, svc.DB,
		`SELECT id FROM seasons WHERE show_id = ? AND season_number = ?`, show.ID, 1)
	if int(*got.SeasonID) != want {
		t.Errorf("season_id = %d, want %d", *got.SeasonID, want)
	}
}

// TestHistorySeasonScope covers the season_id filter: every watch of one
// season's episodes, and nothing else. Breaking Bad is the right fixture
// because it has TWO seasons — a predicate that dropped season_number would
// pass a one-season test and fail here.
func TestHistorySeasonScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, alice, show := trackedShowService(t)
	bob := videotest.SeedUser(t, svc.DB, "bob")

	s1, err := common.EpisodeIDsBySeason(ctx, svc.DB, show.TMDBID, 1)
	if err != nil {
		t.Fatalf("season 1 episode ids: %v", err)
	}
	s2, err := common.EpisodeIDsBySeason(ctx, svc.DB, show.TMDBID, 2)
	if err != nil {
		t.Fatalf("season 2 episode ids: %v", err)
	}
	season1, err := common.GetSeasonByTMDBKey(ctx, svc.DB, show.TMDBID, 1)
	if err != nil {
		t.Fatalf("season 1 row: %v", err)
	}
	season2, err := common.GetSeasonByTMDBKey(ctx, svc.DB, show.TMDBID, 2)
	if err != nil {
		t.Fatalf("season 2 row: %v", err)
	}

	// Season 1 holds one watch; season 2 holds three, one of them a rewatch of
	// the episode already watched — so a join instead of an EXISTS would show
	// duplicates, and a missing row would show as a short page.
	w1 := videotest.SeedWatch(t, svc.DB, alice, "episode", s1[1], "2026-06-01T20:00:00Z")
	w2 := videotest.SeedWatch(t, svc.DB, alice, "episode", s2[1], "2026-06-02T20:00:00Z")
	w3 := videotest.SeedWatch(t, svc.DB, alice, "episode", s2[3], "2026-06-03T20:00:00Z")
	w4 := videotest.SeedWatch(t, svc.DB, alice, "episode", s2[1], "2026-06-04T20:00:00Z")
	// Bob watched season 2 as well, and must be invisible.
	videotest.SeedWatch(t, svc.DB, bob, "episode", s2[1], "2026-06-05T20:00:00Z")
	// A second show alice also watched: the show scope must exclude it, and the
	// season scope must not be reached by it either. Seeded here rather than
	// inside a subtest, because the subtests share this database and run in
	// parallel.
	otherShow := videotest.SeedShow(t, svc.DB, 1400, "Another Show")
	otherEp := videotest.SeedEpisode(t, svc.DB, otherShow, 1, 1, videotest.Ptr("2026-01-01"))
	videotest.SeedWatch(t, svc.DB, alice, "episode", otherEp, "2026-06-06T20:00:00Z")

	history := func(t *testing.T, p HistoryParams) []HistoryEntry {
		t.Helper()
		entries, err := svc.History(ctx, alice, p)
		if err != nil {
			t.Fatalf("History(%+v) error = %v", p, err)
		}
		return entries
	}

	t.Run("scopes to one season's episodes", func(t *testing.T) {
		t.Parallel()
		got := history(t, HistoryParams{SeasonID: videotest.JSONNumber(season1.ID)})
		wantIDs(t, historyIDs(got), []int64{w1})
	})

	t.Run("carries every watch of the season, rewatches included", func(t *testing.T) {
		t.Parallel()
		// Newest first, and w4/w2 are the same episode twice — the rewatch is a
		// separate row (D-009), never collapsed.
		got := history(t, HistoryParams{SeasonID: videotest.JSONNumber(season2.ID)})
		wantIDs(t, historyIDs(got), []int64{w4, w3, w2})
		for _, e := range got {
			if e.Episode == nil || e.Episode.Season != 2 {
				t.Fatalf("entry %d is not a season 2 episode: %+v", e.ID, e.Episode)
			}
		}
	})

	t.Run("paging applies to the scoped feed", func(t *testing.T) {
		t.Parallel()
		got := history(t, HistoryParams{SeasonID: videotest.JSONNumber(season2.ID), Limit: "2"})
		wantIDs(t, historyIDs(got), []int64{w4, w3})
		got = history(t, HistoryParams{SeasonID: videotest.JSONNumber(season2.ID), Limit: "2", Offset: "2"})
		wantIDs(t, historyIDs(got), []int64{w2})
	})

	t.Run("the show scope spans every season, and only this show", func(t *testing.T) {
		t.Parallel()
		// The case that separates a show scope from a season one: w1 is season
		// 1, the rest are season 2, all four belong to this show — and the
		// other show's watch, newer than all of them, must not lead the list.
		got := history(t, HistoryParams{ShowID: videotest.JSONNumber(show.ID)})
		wantIDs(t, historyIDs(got), []int64{w4, w3, w2, w1})
	})

	t.Run("rejects narrowings combined with each other", func(t *testing.T) {
		t.Parallel()
		// At most one narrowing: refused outright rather than letting one
		// silently win.
		combos := []HistoryParams{
			{SeasonID: videotest.JSONNumber(season1.ID), TargetType: "episode", TargetID: videotest.JSONNumber(s1[1])},
			{ShowID: videotest.JSONNumber(show.ID), TargetType: "episode", TargetID: videotest.JSONNumber(s1[1])},
			{ShowID: videotest.JSONNumber(show.ID), SeasonID: videotest.JSONNumber(season1.ID)},
		}
		for _, p := range combos {
			if err := errFrom(svc.History(ctx, alice, p)); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("History(%+v) err = %v, want ErrValidation", p, err)
			}
		}
	})

	t.Run("validates the show id", func(t *testing.T) {
		t.Parallel()
		for _, id := range []string{"0", "-1", "abc"} {
			if err := errFrom(svc.History(ctx, alice, HistoryParams{ShowID: id})); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("show_id %q err = %v, want ErrValidation", id, err)
			}
		}
		err := errFrom(svc.History(ctx, alice, HistoryParams{ShowID: "999999"}))
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("unknown show err = %v, want ErrNotFound", err)
		}
	})

	t.Run("validates the season id", func(t *testing.T) {
		t.Parallel()
		for _, id := range []string{"0", "-1", "abc"} {
			err := errFrom(svc.History(ctx, alice, HistoryParams{SeasonID: id}))
			if !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("season_id %q err = %v, want ErrValidation", id, err)
			}
		}
		// A well-formed id for a season that does not exist is a 404, which the
		// season lookup gives for free.
		err := errFrom(svc.History(ctx, alice, HistoryParams{SeasonID: "999999"}))
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("unknown season err = %v, want ErrNotFound", err)
		}
	})
}

func errFrom(_ []HistoryEntry, err error) error { return err }
