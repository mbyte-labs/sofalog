package lists

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// watchlistKeys renders each entry as "<target_type>:<id>" — the pair that
// actually identifies a target, so an assertion cannot be satisfied by the
// right id in the wrong table.
func watchlistKeys(entries []WatchlistEntry) []string {
	keys := make([]string, len(entries))
	for i, e := range entries {
		t := watchlistTargetOf(e)
		keys[i] = t.TargetType + ":" + fmt.Sprint(t.TargetID)
	}
	return keys
}

// watchlistTargetOf collapses one entry back to the pair that identifies it.
func watchlistTargetOf(e WatchlistEntry) watchlistTarget {
	t := watchlistTarget{TargetType: e.TargetType, AddedAt: e.AddedAt}
	switch {
	case e.Movie != nil:
		t.TargetID = e.Movie.ID
	case e.Show != nil:
		t.TargetID = e.Show.ID
	}
	return t
}

// seedShowSnapshot inserts a catalog show with the columns the watchlist card
// projects: title, first air date, poster and the snapshot its score lives in.
func seedShowSnapshot(t *testing.T, database *db.DB, tmdbID int64, title string,
	firstAir, posterPath *string, metadataJSON string) common.ShowRow {
	t.Helper()
	var row common.ShowRow
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		var err error
		row, err = common.UpsertShowByTMDBID(context.Background(), tx, common.ShowRow{
			TMDBID:       tmdbID,
			Title:        title,
			FirstAirDate: firstAir,
			PosterPath:   posterPath,
			MetadataJSON: metadataJSON,
			FetchedAt:    "2026-08-18T00:00:00Z",
		})
		return err
	})
	if err != nil {
		t.Fatalf("seeding show: %v", err)
	}
	return row
}

// watchlistFixture carries every tripwire the three sort keys can trip:
//
//   - alien and Bright share an EXACT added_at (adding several films in one
//     sitting writes one instant to many rows), so ordering has to fall back to
//     the movie id — this table has no id column of its own to lean on;
//   - "alien electric" is lowercase and "Bright Wire" is capitalised, so a
//     binary TEXT collation would sort every capital ahead of every lowercase
//     and put them the wrong way round;
//   - undated has no release_date, no poster and no community score, exercising
//     all three absent branches and the NULL-placement rule;
//   - dated and undated share nothing else, so a release_date page is
//     unambiguous;
//   - alien is also WATCHED and rated, because the watchlist is an independent
//     axis and both at once is the feature, not a contradiction (D-011);
//   - an episode watch and an episode rating both carry alien's id, so anything
//     predicating on a bare target id renders a visibly wrong row (D-013);
//   - mars is a SHOW, sorting between the movies on every key, and shares alien's
//     id, because show ids and movie ids are separate sequences (D-013) — so a
//     predicate that lost its target_type hands the show alien's watch and
//     alien's rating;
//   - bob has his own row and must be invisible;
//   - one watchlist row points at a movie that is not in the catalog —
//     target_id has no foreign key, so a dangling id is representable.
//   - carol and dave share rows with alice too, for the matching filter
//     (D-028): bob and carol have opted in, dave has not — so dave is neither
//     offered nor accepted, however much his list overlaps.
type watchlistFixture struct {
	svc                     *Service
	alice, bob, carol, dave int64
	alien, bright, undated  common.MovieRow
	mars                    common.ShowRow
}

// seedMatchWatchlist flips the D-028 consent flag directly — the write path
// lives in auth, and this package tests only the read of it.
func seedMatchWatchlist(t *testing.T, database *db.DB, userID int64) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE users SET match_watchlist = 1 WHERE id = ?`, userID)
		return err
	})
	if err != nil {
		t.Fatalf("seeding match_watchlist: %v", err)
	}
}

func newWatchlistFixture(t *testing.T) watchlistFixture {
	t.Helper()
	f := watchlistFixture{svc: &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}}
	f.alien = videotest.SeedMovie(t, f.svc.DB, 348, "alien electric", videotest.Ptr("1979-05-25"), videotest.Ptr("/a.jpg"),
		`{"vote_average": 8.4, "external_ids": {}, "images": {}}`)
	f.bright = videotest.SeedMovie(t, f.svc.DB, 349, "Bright Wire", videotest.Ptr("2001-01-09"), videotest.Ptr("/b.jpg"),
		`{"vote_average": 6.1, "external_ids": {}, "images": {}}`)
	f.undated = videotest.SeedMovie(t, f.svc.DB, 350, "Zero Hour", nil, nil,
		`{"external_ids": {}, "images": {}}`)
	// A show, sorting between alien (1979) and bright (2001) by date and between
	// them by title too. Its id is its own table's — the same number as alien's.
	f.mars = seedShowSnapshot(t, f.svc.DB, 1399, "Mars Signal", videotest.Ptr("1990-03-04"), videotest.Ptr("/m.jpg"),
		`{"vote_average": 7.7, "external_ids": {}, "images": {}}`)
	f.alice = videotest.SeedUser(t, f.svc.DB, "alice")
	f.bob = videotest.SeedUser(t, f.svc.DB, "bob")

	// alien and bright share an instant; undated was listed later.
	videotest.SeedWatchlist(t, f.svc.DB, f.alice, "movie", f.alien.ID, "2026-06-02T20:15:00Z")
	videotest.SeedWatchlist(t, f.svc.DB, f.alice, "movie", f.bright.ID, "2026-06-02T20:15:00Z")
	videotest.SeedWatchlist(t, f.svc.DB, f.alice, "movie", f.undated.ID, "2026-08-15T21:05:00Z")
	videotest.SeedWatchlist(t, f.svc.DB, f.alice, "show", f.mars.ID, "2026-07-20T10:00:00Z")

	videotest.SeedWatch(t, f.svc.DB, f.alice, "movie", f.alien.ID, "2026-07-01T20:00:00Z")
	videotest.SeedRating(t, f.svc.DB, f.alice, "movie", f.alien.ID, 9)

	// Same ids, wrong target_type: both must stay out of alien's entry.
	videotest.SeedWatch(t, f.svc.DB, f.alice, "episode", f.alien.ID, "2026-07-02T20:00:00Z")
	videotest.SeedRating(t, f.svc.DB, f.alice, "episode", f.bright.ID, 2)

	videotest.SeedWatchlist(t, f.svc.DB, f.bob, "movie", f.bright.ID, "2026-06-03T10:00:00Z")
	videotest.SeedWatchlist(t, f.svc.DB, f.alice, "movie", 999999, "2026-07-03T20:00:00Z")

	// Matching (D-028). bob ∩ alice = {bright}; carol ∩ alice = {bright, mars};
	// bob ∩ carol ∩ alice = {bright}. carol's show row shares bright's id in
	// the OTHER table, so a predicate that lost target_type would hand alice a
	// match on alien too. dave overlaps on alien and bright but never opted in.
	seedMatchWatchlist(t, f.svc.DB, f.bob)
	f.carol = videotest.SeedUser(t, f.svc.DB, "carol@example.test")
	seedMatchWatchlist(t, f.svc.DB, f.carol)
	videotest.SeedWatchlist(t, f.svc.DB, f.carol, "movie", f.bright.ID, "2026-06-10T10:00:00Z")
	videotest.SeedWatchlist(t, f.svc.DB, f.carol, "show", f.mars.ID, "2026-06-11T10:00:00Z")
	f.dave = videotest.SeedUser(t, f.svc.DB, "dave")
	videotest.SeedWatchlist(t, f.svc.DB, f.dave, "movie", f.alien.ID, "2026-06-12T10:00:00Z")
	videotest.SeedWatchlist(t, f.svc.DB, f.dave, "movie", f.bright.ID, "2026-06-12T10:00:00Z")
	return f
}

func (f watchlistFixture) watchlist(t *testing.T, p WatchlistParams) []WatchlistEntry {
	t.Helper()
	entries, err := f.svc.Watchlist(context.Background(), f.alice, p)
	if err != nil {
		t.Fatalf("Watchlist(%+v) error = %v", p, err)
	}
	return entries
}

func TestWatchlistOrdering(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)

	// Keys, not bare ids: mars is a SHOW carrying the same id as alien, so a
	// list of numbers could not tell a correct order from a wrong one (D-013).
	movie := func(row common.MovieRow) string { return "movie:" + fmt.Sprint(row.ID) }
	show := func(row common.ShowRow) string { return "show:" + fmt.Sprint(row.ID) }

	tests := []struct {
		name     string
		params   WatchlistParams
		wantKeys []string
	}{
		{
			// Default: most recently added first, id descending within the
			// shared instant.
			name:     "defaults to added_at then id descending",
			params:   WatchlistParams{},
			wantKeys: []string{movie(f.undated), show(f.mars), movie(f.bright), movie(f.alien)},
		},
		{
			name:     "ascending reverses both keys",
			params:   WatchlistParams{Order: "asc"},
			wantKeys: []string{movie(f.alien), movie(f.bright), show(f.mars), movie(f.undated)},
		},
		{
			// Case-insensitive: a binary collation would put "Bright Wire",
			// "Mars Signal" and "Zero Hour" ahead of "alien electric".
			name:     "title ascending ignores case and mixes both kinds",
			params:   WatchlistParams{OrderBy: "title", Order: "asc"},
			wantKeys: []string{movie(f.alien), movie(f.bright), show(f.mars), movie(f.undated)},
		},
		{
			name:     "title descending ignores case",
			params:   WatchlistParams{OrderBy: "title", Order: "desc"},
			wantKeys: []string{movie(f.undated), show(f.mars), movie(f.bright), movie(f.alien)},
		},
		{
			// A show's release date is its FIRST AIR DATE, so mars sorts
			// between the two dated films. The undated film goes LAST, not
			// first — SQLite would sort NULLs first here without the IS NULL
			// prefix.
			name:     "release_date ascending sorts a show on its first air date",
			params:   WatchlistParams{OrderBy: "release_date", Order: "asc"},
			wantKeys: []string{movie(f.alien), show(f.mars), movie(f.bright), movie(f.undated)},
		},
		{
			// And undated stays last descending too: the IS NULL key does not
			// flip, so an undated item never leads a page in either direction.
			name:     "release_date descending keeps undated last",
			params:   WatchlistParams{OrderBy: "release_date", Order: "desc"},
			wantKeys: []string{movie(f.bright), show(f.mars), movie(f.alien), movie(f.undated)},
		},
		{
			name:     "type=movie excludes the show",
			params:   WatchlistParams{Type: "movie"},
			wantKeys: []string{movie(f.undated), movie(f.bright), movie(f.alien)},
		},
		{
			name:     "type=show is only the show",
			params:   WatchlistParams{Type: "show"},
			wantKeys: []string{show(f.mars)},
		},
		{
			name:     "type=all is the same as omitting it",
			params:   WatchlistParams{Type: "all"},
			wantKeys: []string{movie(f.undated), show(f.mars), movie(f.bright), movie(f.alien)},
		},
		{
			name:     "limit takes the first page after ordering",
			params:   WatchlistParams{Limit: "2"},
			wantKeys: []string{movie(f.undated), show(f.mars)},
		},
		{
			name:     "offset skips into the ordered list",
			params:   WatchlistParams{Limit: "2", Offset: "2"},
			wantKeys: []string{movie(f.bright), movie(f.alien)},
		},
		{
			name:     "offset past the end is an empty page, not an error",
			params:   WatchlistParams{Limit: "20", Offset: "40"},
			wantKeys: []string{},
		},
		{
			name:     "limit larger than the list returns everything",
			params:   WatchlistParams{Limit: "50"},
			wantKeys: []string{movie(f.undated), show(f.mars), movie(f.bright), movie(f.alien)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := watchlistKeys(f.watchlist(t, tt.params))
			if !slices.Equal(got, tt.wantKeys) {
				t.Fatalf("order = %v, want %v", got, tt.wantKeys)
			}
		})
	}
}

// The show arm of the payload, which was declared and always null until shows
// became watchlistable. mars shares alien's id, so every assertion here is also
// a D-013 assertion: alien's watch and alien's 9 must not reach the show.
func TestWatchlistShowArm(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)

	entries := f.watchlist(t, WatchlistParams{Type: "show"})
	if len(entries) != 1 {
		t.Fatalf("len = %d, want the one show row", len(entries))
	}
	e := entries[0]
	if e.TargetType != "show" || e.Show == nil || e.Movie != nil {
		t.Fatalf("entry = %+v, want the show arm filled and the movie arm null", e)
	}
	if e.Show.ID != f.mars.ID || e.Show.Name != "Mars Signal" {
		t.Errorf("show = %+v, want mars", e.Show)
	}
	if e.Show.FirstAirDate == nil || *e.Show.FirstAirDate != "1990-03-04" {
		t.Errorf("first_air_date = %v, want the seeded date", e.Show.FirstAirDate)
	}
	if e.AddedAt != "2026-07-20T10:00:00Z" {
		t.Errorf("added_at = %q, want the seeded instant", e.AddedAt)
	}
	if e.Poster == nil || !strings.Contains(*e.Poster, "w185/m.jpg") {
		t.Errorf("poster = %v, want the proxy URL for /m.jpg", e.Poster)
	}
	if e.PosterFull == nil || !strings.Contains(*e.PosterFull, "w500/m.jpg") {
		t.Errorf("poster_full = %v, want the w500 proxy URL for /m.jpg", e.PosterFull)
	}
	if e.Rating == nil || *e.Rating != 7.7 {
		t.Errorf("rating = %v, want the show snapshot's 7.7", e.Rating)
	}
	// alien's state, on the same id in the other table.
	if e.Watched {
		t.Error("watched = true, want false — that watch is on the movie with this id")
	}
	if e.UserRating != nil {
		t.Errorf("user_rating = %v, want nil — that rating is on the movie with this id", *e.UserRating)
	}
}

// A show's `watched` means COMPLETED — the stored status reached 'watched'
// (every aired, non-special episode, D-012). "Watching" is not a ✓.
func TestWatchlistShowWatchedMeansCompleted(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)
	ctx := context.Background()

	showEntry := func() WatchlistEntry {
		t.Helper()
		entries := f.watchlist(t, WatchlistParams{Type: "show"})
		if len(entries) != 1 {
			t.Fatalf("len = %d, want 1", len(entries))
		}
		return entries[0]
	}

	videotest.SeedShowStatus(t, f.svc.DB, f.alice, f.mars.ID, "watching")
	if showEntry().Watched {
		t.Error("watched = true while watching, want false")
	}

	if err := f.svc.DB.InTx(ctx, func(tx db.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE user_show_states SET status = 'watched'
		                        WHERE user_id = ? AND show_id = ?`, f.alice, f.mars.ID)
		return err
	}); err != nil {
		t.Fatalf("promoting the status: %v", err)
	}
	e := showEntry()
	if !e.Watched {
		t.Error("watched = false once the status is watched, want true")
	}
	// Both axes at once is the feature, not a contradiction (D-011).
	if e.Show == nil {
		t.Error("a watched show left the watchlist, want it listed still")
	}
}

// target_id has no foreign key, so a watchlist row can outlive its catalog row.
// The join drops it BEFORE the limit, so a page is exactly as long as the rows
// it returns.
func TestWatchlistOmitsRowWithNoCatalogRow(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)

	entries := f.watchlist(t, WatchlistParams{})
	if len(entries) != 4 {
		t.Fatalf("len = %d, want 4 — the dangling row must be dropped", len(entries))
	}
	// A limit equal to the surviving count must still fill the page.
	if got := f.watchlist(t, WatchlistParams{Limit: "4"}); len(got) != 4 {
		t.Fatalf("limit=4 returned %d, want 4 — the dangling row consumed a slot", len(got))
	}
}

func TestWatchlistIsPerUser(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)

	entries, err := f.svc.Watchlist(context.Background(), f.bob, WatchlistParams{})
	if err != nil {
		t.Fatalf("Watchlist error = %v", err)
	}
	if len(entries) != 1 || entries[0].Movie.ID != f.bright.ID {
		t.Fatalf("bob's list = %+v, want only his own row", entries)
	}
	// Alice rated and watched nothing of bob's, and her state must not bleed in.
	if entries[0].Watched || entries[0].UserRating != nil {
		t.Errorf("bob's entry carries alice's state: watched=%v user_rating=%v",
			entries[0].Watched, entries[0].UserRating)
	}
}

// The two axes are independent (D-011), and every polymorphic predicate carries
// its target_type (D-013) — the fixture's episode watch and episode rating both
// reuse a movie id precisely so a missing target_type shows up here.
func TestWatchlistProjectsPerTargetState(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)

	byID := map[int64]WatchlistEntry{}
	for _, e := range f.watchlist(t, WatchlistParams{}) {
		if e.Movie != nil { // the show arm has its own test
			byID[e.Movie.ID] = e
		}
	}

	alien := byID[f.alien.ID]
	if !alien.Watched {
		t.Error("alien watched = false, want true — watchlisted and watched at once is the feature")
	}
	if alien.UserRating == nil || *alien.UserRating != 9 {
		t.Errorf("alien user_rating = %v, want 9", alien.UserRating)
	}
	if alien.Rating == nil || *alien.Rating != 8.4 {
		t.Errorf("alien rating = %v, want the TMDB score 8.4", alien.Rating)
	}
	if alien.Poster == nil || alien.PosterFull == nil {
		t.Error("alien poster or poster_full = nil, want the proxy URLs for /a.jpg")
	}
	if alien.AddedAt != "2026-06-02T20:15:00Z" {
		t.Errorf("alien added_at = %q, want the seeded instant", alien.AddedAt)
	}

	// bright's only rating is on EPISODE bright.ID, and nothing watched it.
	bright := byID[f.bright.ID]
	if bright.UserRating != nil {
		t.Errorf("bright user_rating = %v, want nil — that rating is on an episode", *bright.UserRating)
	}
	if bright.Watched {
		t.Error("bright watched = true, want false")
	}

	undated := byID[f.undated.ID]
	if undated.Movie.ReleaseDate != nil || undated.Poster != nil || undated.PosterFull != nil || undated.Rating != nil {
		t.Errorf("undated = %+v, want every absent branch null", undated)
	}
}

func TestWatchlistInvalidParams(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)

	invalid := []struct {
		name   string
		params WatchlistParams
	}{
		{"unknown order_by", WatchlistParams{OrderBy: "popularity"}},
		{"order_by is not a column name", WatchlistParams{OrderBy: "added_at"}},
		{"unknown order", WatchlistParams{Order: "sideways"}},
		{"unknown type", WatchlistParams{Type: "banana"}},
		{"episode is not a watchlist target", WatchlistParams{Type: "episode"}},
		{"limit zero", WatchlistParams{Limit: "0"}},
		{"limit negative", WatchlistParams{Limit: "-1"}},
		{"limit not a number", WatchlistParams{Limit: "many"}},
		{"offset negative", WatchlistParams{Offset: "-1"}},
		{"offset not a number", WatchlistParams{Offset: "later"}},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := f.svc.Watchlist(context.Background(), f.alice, tt.params)
			if !errors.Is(err, httpx.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestWatchlistMatchUsers(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)

	// Only the opted-in OTHERS, by label: carol has no display name, so the
	// label is her email's local part — and the email itself never travels.
	users, err := f.svc.WatchlistMatchUsers(context.Background(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	want := []MatchUser{{ID: f.bob, Label: "bob"}, {ID: f.carol, Label: "carol"}}
	if !slices.Equal(users, want) {
		t.Errorf("match users = %+v, want %+v", users, want)
	}

	// bob's own view excludes bob and still excludes dave.
	users, err = f.svc.WatchlistMatchUsers(context.Background(), f.bob)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(users, []MatchUser{{ID: f.carol, Label: "carol"}}) {
		t.Errorf("bob's match users = %+v, want carol only", users)
	}

	// Nobody sharing is an empty array, never null.
	seedMatchWatchlist(t, f.svc.DB, f.dave)
	if users, err := f.svc.WatchlistMatchUsers(context.Background(), f.dave); err != nil || users == nil || len(users) != 2 {
		t.Errorf("dave's match users = %+v, %v; want bob and carol", users, err)
	}
}

func TestWatchlistMatching(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)
	movie := func(row common.MovieRow) string { return "movie:" + fmt.Sprint(row.ID) }
	show := func(row common.ShowRow) string { return "show:" + fmt.Sprint(row.ID) }
	id := func(n int64) string { return fmt.Sprint(n) }

	tests := []struct {
		name     string
		params   WatchlistParams
		wantKeys []string
	}{
		{
			name:     "one user is the pairwise intersection",
			params:   WatchlistParams{MatchUsers: []string{id(f.bob)}},
			wantKeys: []string{movie(f.bright)},
		},
		{
			// mars is on carol's list; a predicate without target_type would
			// also match alien (movie 5 vs show 5, D-013).
			name:     "a show matches only against a show row",
			params:   WatchlistParams{MatchUsers: []string{id(f.carol)}},
			wantKeys: []string{show(f.mars), movie(f.bright)},
		},
		{
			name:     "several users is ALL of them, not any",
			params:   WatchlistParams{MatchUsers: []string{id(f.bob), id(f.carol)}},
			wantKeys: []string{movie(f.bright)},
		},
		{
			name:     "combines with type",
			params:   WatchlistParams{MatchUsers: []string{id(f.carol)}, Type: "show"},
			wantKeys: []string{show(f.mars)},
		},
		{
			name:     "combines with ordering",
			params:   WatchlistParams{MatchUsers: []string{id(f.carol)}, OrderBy: "title", Order: "asc"},
			wantKeys: []string{movie(f.bright), show(f.mars)},
		},
		{
			name:     "an empty intersection is an empty list",
			params:   WatchlistParams{MatchUsers: []string{id(f.bob)}, Type: "show"},
			wantKeys: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := watchlistKeys(f.watchlist(t, tt.params))
			if !slices.Equal(got, tt.wantKeys) {
				t.Fatalf("keys = %v, want %v", got, tt.wantKeys)
			}
		})
	}

	// The entry stays ALICE's: her added_at, her rating, her watched flag —
	// nothing of bob's leaks through the match.
	t.Run("entries carry the caller's own state", func(t *testing.T) {
		t.Parallel()
		entries := f.watchlist(t, WatchlistParams{MatchUsers: []string{id(f.bob)}})
		if len(entries) != 1 || entries[0].AddedAt != "2026-06-02T20:15:00Z" {
			t.Fatalf("entries = %+v, want alice's bright row with alice's added_at", entries)
		}
	})

	// The SQL guard, independent of the service check: a user who opted out
	// after being validated matches nothing rather than leaking.
	t.Run("the consent check lives in the query too", func(t *testing.T) {
		t.Parallel()
		targets, err := listWatchlistTargets(context.Background(), f.svc.DB, watchlistFilter{
			UserID: f.alice, OrderBy: "added", MatchUsers: []int64{f.dave},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 0 {
			t.Errorf("targets = %+v, want none for a user who never opted in", targets)
		}
	})
}

func TestWatchlistMatchingInvalid(t *testing.T) {
	t.Parallel()
	f := newWatchlistFixture(t)
	id := func(n int64) string { return fmt.Sprint(n) }
	eleven := make([]string, 11)
	for i := range eleven {
		eleven[i] = fmt.Sprint(1000 + i)
	}

	invalid := []struct {
		name  string
		users []string
		want  string
	}{
		{"not a number", []string{"bob"}, "positive integer"},
		{"zero", []string{"0"}, "positive integer"},
		{"yourself", []string{id(f.alice)}, "yourself"},
		{"repeated", []string{id(f.bob), id(f.bob)}, "repeated"},
		{"not sharing", []string{id(f.dave)}, "not sharing"},
		{"unknown user", []string{"424242"}, "not sharing"},
		{"one sharing, one not", []string{id(f.bob), id(f.dave)}, "not sharing"},
		{"over the cap", eleven, "at most"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := f.svc.Watchlist(context.Background(), f.alice, WatchlistParams{MatchUsers: tt.users})
			if !errors.Is(err, httpx.ErrValidation) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want ErrValidation mentioning %q", err, tt.want)
			}
		})
	}
}
