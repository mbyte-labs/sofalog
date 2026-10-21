package restore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// Entry builders: the file's shape, without the noise of every pointer.

func tbAddr(typ string, tmdb int64) TicketBoxAddress {
	return TicketBoxAddress{Type: typ, TMDBID: &tmdb, IMDBID: videotest.Ptr("tt0133093")}
}

func tbEpisodeAddr(show int64, season, episode int) TicketBoxAddress {
	return TicketBoxAddress{Type: "episode", TMDBID: videotest.Ptr(int64(9)), IMDBID: videotest.Ptr("tt0959621"),
		ShowTMDBID: &show, Season: &season, Episode: &episode}
}

func tbMovieWatch(tmdb int64, at string) TicketBoxHistoryEntry {
	return TicketBoxHistoryEntry{TicketBoxAddress: tbAddr("movie", tmdb), AddedAt: at, HistoryType: "watch"}
}

func tbEpisodeWatch(show int64, season, episode int, at string) TicketBoxHistoryEntry {
	return TicketBoxHistoryEntry{TicketBoxAddress: tbEpisodeAddr(show, season, episode), AddedAt: at, HistoryType: "watch"}
}

func tbRating(typ string, tmdb int64, rating int, at string) TicketBoxRatingEntry {
	return TicketBoxRatingEntry{TicketBoxAddress: tbAddr(typ, tmdb), Rating: &rating, RatedAt: at}
}

func tbEpisodeRating(show int64, season, episode, rating int, at string) TicketBoxRatingEntry {
	return TicketBoxRatingEntry{TicketBoxAddress: tbEpisodeAddr(show, season, episode), Rating: &rating, RatedAt: at}
}

func tbWatchlist(typ string, tmdb int64, at string) TicketBoxWatchlistEntry {
	return TicketBoxWatchlistEntry{TicketBoxAddress: tbAddr(typ, tmdb), ListedAt: at}
}

func tbFavorite(typ string, tmdb int64, at string) TicketBoxFavoriteEntry {
	return TicketBoxFavoriteEntry{TicketBoxAddress: tbAddr(typ, tmdb), FavoritedAt: at}
}

func tbDump(history []TicketBoxHistoryEntry, ratings []TicketBoxRatingEntry, watchlist []TicketBoxWatchlistEntry) TicketBoxDump {
	return TicketBoxDump{SchemaVersion: 5, ExportedAt: "2026-08-19T10:00:00.000Z",
		History: history, Ratings: ratings, Watchlist: watchlist}
}

// tbFavorites is tbDump with a favorites section, the one array most cases
// do not need.
func tbFavorites(d TicketBoxDump, favorites ...TicketBoxFavoriteEntry) TicketBoxDump {
	d.Favorites = favorites
	return d
}

// runRestore drives the import synchronously against svc and returns the
// final snapshot — the runner is bypassed, so nothing here is asynchronous.
func runRestore(t *testing.T, svc *Service, userID int64, dump TicketBoxDump) RestoreJob {
	t.Helper()
	now := svc.Now().UTC()
	plan, skips := ValidateTicketBoxDump(dump, now)
	job := NewRestoreJob(now, plan, skips)
	svc.RunTicketBoxRestore(context.Background(), userID, plan, job)
	return job.Snapshot()
}

func restoreService(t *testing.T, stub *videotest.ProviderStub) (*Service, int64) {
	t.Helper()
	svc, _ := detailsService(t, stub)
	return svc, videotest.SeedUser(t, svc.DB, "alice")
}

func TestNormaliseInstant(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name, raw, want string
		err             error
	}{
		{"plain UTC", "2026-08-01T10:00:00Z", "2026-08-01T10:00:00Z", nil},
		{"millis are dropped", "2026-08-01T10:00:00.123Z", "2026-08-01T10:00:00Z", nil},
		{"an offset is folded into UTC", "2026-08-01T12:00:00+02:00", "2026-08-01T10:00:00Z", nil},
		{"the distant past is fine", "2002-04-01T17:42:00.000Z", "2002-04-01T17:42:00Z", nil},
		{"now itself is fine", "2026-08-19T12:00:00Z", "2026-08-19T12:00:00Z", nil},
		{"the future is refused", "2026-08-19T12:00:01Z", "", common.ErrInstantFuture},
		{"no offset is unparsable", "2026-08-01T10:00:00.000", "", common.ErrInstantUnparsable},
		{"garbage is unparsable", "yesterday", "", common.ErrInstantUnparsable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := common.NormaliseInstant(tc.raw, now)
			if !errors.Is(err, tc.err) || got != tc.want {
				t.Errorf("normaliseInstant(%q) = %q, %v; want %q, %v", tc.raw, got, err, tc.want, tc.err)
			}
		})
	}
}

func TestParseTicketBoxDump(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body, wantErr string
	}{
		{"version 1 names the version", `{"schemaVersion": 1, "history": []}`, "schemaVersion 1 is not supported"},
		{"version 2 names the version", `{"schemaVersion": 2, "history": []}`, "schemaVersion 2 is not supported"},
		{"missing version", `{"history": []}`, "schemaVersion is required"},
		{"not an object", `[1, 2]`, "one JSON object"},
		{"unknown top-level field", `{"schemaVersion": 5, "bookmarks": []}`, `unknown field "bookmarks"`},
		{"unknown entry field", `{"schemaVersion": 5, "history": [{"type": "movie", "tmdbId": 1, "rewatch": true}]}`, `unknown field "rewatch"`},
		{"a trakt id is gone since version 3", `{"schemaVersion": 5, "history": [{"type": "movie", "traktId": 1, "tmdbId": 1}]}`, `unknown field "traktId"`},
		{"unknown favorite field", `{"schemaVersion": 5, "favorites": [{"type": "movie", "tmdbId": 1, "listedAt": "x"}]}`, `unknown field "listedAt"`},
		{"wrong type", `{"schemaVersion": 5, "history": [{"type": "movie", "tmdbId": "603"}]}`, "cannot unmarshal"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseTicketBoxDump(json.RawMessage(tc.body))
			if !errors.Is(err, httpx.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}

	t.Run("a version-5 file decodes, missing arrays read as empty", func(t *testing.T) {
		t.Parallel()
		d, err := parseTicketBoxDump(json.RawMessage(`{"schemaVersion": 5, "exportedAt": "x",
			"history": [{"type": "episode", "tmdbId": null, "imdbId": null, "showTmdbId": 1396,
			             "season": 2, "episode": 1, "addedAt": "2026-08-01T10:00:00.000Z", "historyType": "checkin"}],
			"favorites": [{"type": "show", "tmdbId": 1396, "imdbId": "tt0903747", "favoritedAt": "2026-08-01T10:00:00.000Z"}]}`))
		if err != nil {
			t.Fatalf("parseTicketBoxDump: %v", err)
		}
		if len(d.History) != 1 || d.History[0].TMDBID != nil || *d.History[0].ShowTMDBID != 1396 || d.History[0].HistoryType != "checkin" {
			t.Errorf("history = %+v", d.History)
		}
		if len(d.Favorites) != 1 || d.Favorites[0].Type != "show" || *d.Favorites[0].TMDBID != 1396 || d.Favorites[0].FavoritedAt == "" {
			t.Errorf("favorites = %+v", d.Favorites)
		}
		if d.Ratings != nil || d.Watchlist != nil {
			t.Errorf("absent arrays decoded as %v / %v, want nil", d.Ratings, d.Watchlist)
		}
	})
}

func TestEpisodeSkipHint(t *testing.T) {
	t.Parallel()
	gap := []int{1, 2, 3, 5, 6} // TMDB merged 3 and 4 into 3
	tests := []struct {
		name            string
		numbers         []int
		season, episode int
		want            string
		wantLast        int // 0 = null
	}{
		{"specials are numbered their own way", gap, 0, 4, hintSpecialsNumbering, 0},
		{"a season TMDB does not list", nil, 9, 1, hintSeasonMissing, 0},
		{"the number after the season's last", gap, 3, 7, hintPastSeasonEnd, 6},
		{"far past the season's end", gap, 3, 22, hintPastSeasonEnd, 6},
		{"a number missing mid-season", gap, 3, 4, hintNumberingGap, 6},
		{"a missing first episode", []int{2, 3}, 1, 1, hintNumberingGap, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, last := episodeSkipHint(tc.numbers, tc.season, tc.episode)
			gotLast := 0
			if last != nil {
				gotLast = *last
			}
			if got != tc.want || gotLast != tc.wantLast {
				t.Errorf("episodeSkipHint = %s / %d, want %s / %d", got, gotLast, tc.want, tc.wantLast)
			}
		})
	}
}

func TestValidateTicketBoxDump(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	const ok = "2026-08-01T10:00:00.000Z"

	t.Run("one skip reason per refusal", func(t *testing.T) {
		t.Parallel()
		noID := tbMovieWatch(603, ok)
		noID.TMDBID = nil
		badType := tbMovieWatch(603, ok)
		badType.HistoryType = "plex"
		noPos := tbEpisodeWatch(1396, 2, 1, ok)
		noPos.Episode = nil
		negPos := tbEpisodeWatch(1396, 2, -1, ok)
		noShow := tbEpisodeWatch(1396, 2, 1, ok)
		noShow.ShowTMDBID = nil
		noRating := tbRating("movie", 603, 5, ok)
		noRating.Rating = nil

		tests := []struct {
			name string
			dump TicketBoxDump
			kind string
			want string
		}{
			{"movie watch without tmdbId", tbDump([]TicketBoxHistoryEntry{noID}, nil, nil), "history", skipMissingTMDBID},
			{"unknown history type", tbDump([]TicketBoxHistoryEntry{{TicketBoxAddress: tbAddr("show", 1396), AddedAt: ok, HistoryType: "watch"}}, nil, nil), "history", skipUnknownType},
			{"unknown watch type", tbDump([]TicketBoxHistoryEntry{badType}, nil, nil), "history", skipInvalidWatchType},
			{"episode without position", tbDump([]TicketBoxHistoryEntry{noPos}, nil, nil), "history", skipMissingPosition},
			{"episode with negative position", tbDump([]TicketBoxHistoryEntry{negPos}, nil, nil), "history", skipMissingPosition},
			{"episode without show id", tbDump([]TicketBoxHistoryEntry{noShow}, nil, nil), "history", skipMissingTMDBID},
			{"bad timestamp", tbDump([]TicketBoxHistoryEntry{tbMovieWatch(603, "2026-08-01T10:00:00.000")}, nil, nil), "history", skipInvalidTimestamp},
			{"future timestamp", tbDump([]TicketBoxHistoryEntry{tbMovieWatch(603, "2027-01-01T00:00:00Z")}, nil, nil), "history", skipFutureTimestamp},
			{"season rating", tbDump(nil, []TicketBoxRatingEntry{tbRating("season", 5, 5, ok)}, nil), "rating", skipSeasonRating},
			{"rating out of range", tbDump(nil, []TicketBoxRatingEntry{tbRating("movie", 603, 11, ok)}, nil), "rating", skipRatingOutOfRange},
			{"rating missing", tbDump(nil, []TicketBoxRatingEntry{noRating}, nil), "rating", skipRatingOutOfRange},
			{"rating of unknown type", tbDump(nil, []TicketBoxRatingEntry{tbRating("game", 1, 5, ok)}, nil), "rating", skipUnknownType},
			{"rating without tmdbId", tbDump(nil, []TicketBoxRatingEntry{{TicketBoxAddress: TicketBoxAddress{Type: "show"}, Rating: videotest.Ptr(5), RatedAt: ok}}, nil), "rating", skipMissingTMDBID},
			{"episode on the watchlist", tbDump(nil, nil, []TicketBoxWatchlistEntry{{TicketBoxAddress: tbEpisodeAddr(1396, 1, 1), ListedAt: ok}}), "watchlist", skipWatchlistTarget},
			{"season on the watchlist", tbDump(nil, nil, []TicketBoxWatchlistEntry{tbWatchlist("season", 5, ok)}), "watchlist", skipWatchlistTarget},
			{"watchlist without tmdbId", tbDump(nil, nil, []TicketBoxWatchlistEntry{{TicketBoxAddress: TicketBoxAddress{Type: "movie"}, ListedAt: ok}}), "watchlist", skipMissingTMDBID},
			{"watchlist future timestamp", tbDump(nil, nil, []TicketBoxWatchlistEntry{tbWatchlist("movie", 603, "2027-01-01T00:00:00Z")}), "watchlist", skipFutureTimestamp},
			{"favorite of unknown type", tbFavorites(tbDump(nil, nil, nil), tbFavorite("season", 5, ok)), "favorite", skipUnknownType},
			{"favorite without tmdbId", tbFavorites(tbDump(nil, nil, nil), TicketBoxFavoriteEntry{TicketBoxAddress: TicketBoxAddress{Type: "movie"}, FavoritedAt: ok}), "favorite", skipMissingTMDBID},
			{"favorite bad timestamp", tbFavorites(tbDump(nil, nil, nil), tbFavorite("show", 1396, "soon")), "favorite", skipInvalidTimestamp},
			{"favorite future timestamp", tbFavorites(tbDump(nil, nil, nil), tbFavorite("movie", 603, "2027-01-01T00:00:00Z")), "favorite", skipFutureTimestamp},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				plan, skips := ValidateTicketBoxDump(tc.dump, now)
				if len(skips) != 1 || skips[0].Reason != tc.want || skips[0].Kind != tc.kind {
					t.Fatalf("skips = %+v, want one %s/%s", skips, tc.kind, tc.want)
				}
				if len(plan.movies)+len(plan.shows) != 0 {
					t.Errorf("plan has %d movies and %d shows, want none", len(plan.movies), len(plan.shows))
				}
			})
		}
	})

	t.Run("entries regroup by catalog item, sorted, with the last rating winning", func(t *testing.T) {
		t.Parallel()
		plan, skips := ValidateTicketBoxDump(tbFavorites(tbDump(
			[]TicketBoxHistoryEntry{
				tbMovieWatch(603, "2026-08-01T10:00:00.000Z"),
				tbMovieWatch(603, "2026-08-01T10:00:00.000Z"), // a duplicate row is kept
				tbEpisodeWatch(1396, 2, 1, "2026-08-02T10:00:00.000Z"),
				tbMovieWatch(11, "2026-08-03T12:00:00+02:00"),
			},
			[]TicketBoxRatingEntry{
				tbRating("movie", 603, 5, "2026-08-01T10:00:00.000Z"),
				tbRating("movie", 603, 8, "2026-08-02T10:00:00.000Z"),
				tbRating("show", 2000, 7, "2026-08-02T10:00:00.000Z"), // a rating-only show
				tbEpisodeRating(1396, 2, 4, 9, "2026-08-02T10:00:00.000Z"),
				tbEpisodeRating(1396, 1, 1, 6, "2026-08-02T10:00:00.000Z"),
			},
			[]TicketBoxWatchlistEntry{tbWatchlist("show", 1396, "2026-08-04T10:00:00.000Z")},
		),
			tbFavorite("movie", 603, "2026-08-05T10:00:00.000Z"),
			tbFavorite("show", 3000, "2026-08-05T10:00:00.000Z"), // a favorite-only show
		), now)
		if len(skips) != 0 {
			t.Fatalf("skips = %+v, want none", skips)
		}
		if len(plan.movies) != 2 || plan.movies[0].tmdbID != 11 || plan.movies[1].tmdbID != 603 {
			t.Fatalf("movies = %+v, want [11 603]", plan.movies)
		}
		matrix := plan.movies[1]
		if len(matrix.watches) != 2 || matrix.rating == nil || matrix.rating.value != 8 || matrix.watchlistAt != nil ||
			matrix.favoriteAt == nil || *matrix.favoriteAt != "2026-08-05T10:00:00Z" {
			t.Errorf("movie 603 = %+v", matrix)
		}
		if plan.movies[0].watches[0].at != "2026-08-03T10:00:00Z" {
			t.Errorf("offset not folded: %q", plan.movies[0].watches[0].at)
		}
		if len(plan.shows) != 3 || plan.shows[0].tmdbID != 1396 || plan.shows[1].tmdbID != 2000 || plan.shows[2].tmdbID != 3000 {
			t.Fatalf("shows = %+v, want [1396 2000 3000]", plan.shows)
		}
		if fav := plan.shows[2]; len(fav.watches) != 0 || fav.showRating != nil || fav.favoriteAt == nil {
			t.Errorf("favorite-only show = %+v, want only a favorite", fav)
		}
		bb := plan.shows[0]
		if len(bb.watches) != 1 || bb.showRating != nil || bb.watchlistAt == nil || len(bb.episodeRatings) != 2 {
			t.Errorf("show 1396 = %+v", bb)
		}
		if bb.episodeRatings[0].season != 1 || bb.episodeRatings[1].episode != 4 {
			t.Errorf("episode ratings not sorted by position: %+v", bb.episodeRatings)
		}
		if other := plan.shows[1]; len(other.watches) != 0 || other.showRating == nil {
			t.Errorf("rating-only show = %+v, want no watches and a rating", other)
		}
	})
}

func TestRunTicketBoxRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const at = "2026-08-01T10:00:00.000Z"

	t.Run("replaces the named items' watches and leaves everything else alone", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/movie/603": videotest.MatrixDetailsBody, "/3/tv/1396": videotest.ShowWithSeasonBody}}
		svc, alice := restoreService(t, stub)
		bob := videotest.SeedUser(t, svc.DB, "bob")

		// Existing state: the matrix and Breaking Bad are already tracked with
		// watches by both users; a second show is not in the file at all.
		matrix := videotest.SeedMovie(t, svc.DB, 603, "The Matrix", nil, nil, `{"external_ids": {}, "images": {}}`)
		bbID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		bbS1E1 := videotest.SeedEpisode(t, svc.DB, bbID, 1, 1, videotest.Ptr("2008-01-20"))
		bbS2E1 := videotest.SeedEpisode(t, svc.DB, bbID, 2, 1, videotest.Ptr("2009-03-08"))
		videotest.SeedEpisode(t, svc.DB, bbID, 2, 2, nil)
		videotest.SeedEpisode(t, svc.DB, bbID, 2, 3, videotest.Ptr("2009-03-22"))
		videotest.SeedEpisode(t, svc.DB, bbID, 2, 4, videotest.Ptr("2009-05-31"))
		otherID := videotest.SeedShow(t, svc.DB, 999, "Other Show")
		otherEp := videotest.SeedEpisode(t, svc.DB, otherID, 1, 1, videotest.Ptr("2010-01-01"))

		videotest.SeedWatch(t, svc.DB, alice, "movie", matrix.ID, "2020-01-01T00:00:00Z")
		videotest.SeedWatch(t, svc.DB, alice, "movie", matrix.ID, "2021-01-01T00:00:00Z")
		videotest.SeedWatch(t, svc.DB, alice, "episode", bbS1E1, "2020-01-01T00:00:00Z")
		videotest.SeedWatch(t, svc.DB, alice, "episode", otherEp, "2020-01-01T00:00:00Z")
		videotest.SeedWatch(t, svc.DB, bob, "movie", matrix.ID, "2020-01-01T00:00:00Z")
		videotest.SeedWatch(t, svc.DB, bob, "episode", bbS1E1, "2020-01-01T00:00:00Z")

		checkin := tbMovieWatch(603, "2019-05-05T20:00:00.123Z")
		checkin.HistoryType = "checkin"
		job := runRestore(t, svc, alice, tbFavorites(tbDump(
			[]TicketBoxHistoryEntry{
				checkin,
				tbEpisodeWatch(1396, 1, 1, "2018-01-01T22:00:00+02:00"),
				tbEpisodeWatch(1396, 2, 1, at),
			},
			[]TicketBoxRatingEntry{
				tbRating("movie", 603, 9, at),
				tbRating("show", 1396, 8, at),
				tbEpisodeRating(1396, 2, 1, 7, at),
			},
			[]TicketBoxWatchlistEntry{tbWatchlist("show", 1396, at)},
		), tbFavorite("movie", 603, at), tbFavorite("show", 1396, at)))

		if job.State != "done" || job.Error != nil {
			t.Fatalf("job = %+v", job)
		}
		r := job.Report
		if r.HistoryInserted != 3 || r.HistoryReplaced != 3 || r.RatingsUpserted != 3 || r.WatchlistInserted != 1 || r.FavoritesInserted != 2 {
			t.Errorf("report = %+v", r)
		}
		if r.MoviesMaterialised != 0 || r.ShowsMaterialised != 0 || stub.CallCount("/3/movie/603") != 0 || stub.CallCount("/3/tv/1396") != 0 {
			t.Errorf("tracked items cost TMDB calls: report %+v, calls %v", r, stub.Calls)
		}
		if r.SkippedTotal != 0 {
			t.Errorf("skipped = %+v, want none", r.Skipped)
		}
		if job.Progress.GroupsDone != 2 || job.Progress.GroupsTotal != 2 {
			t.Errorf("progress = %+v", job.Progress)
		}

		// Alice's matrix log is exactly the file's one row, with its
		// provenance and its instant normalised (millis dropped).
		var watchedAt, watchType string
		err := svc.DB.ReadRow(ctx, `SELECT watched_at, watch_type FROM user_watch_history
			WHERE user_id = ? AND target_type = 'movie' AND target_id = ?`, alice, matrix.ID).Scan(&watchedAt, &watchType)
		if err != nil || watchedAt != "2019-05-05T20:00:00Z" || watchType != "checkin" {
			t.Errorf("matrix watch = %q %q %v", watchedAt, watchType, err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history WHERE user_id = ? AND target_type = 'movie'`, alice); n != 1 {
			t.Errorf("alice movie watches = %d, want 1", n)
		}
		// The offset folded into UTC on the episode row.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history
			WHERE user_id = ? AND target_type = 'episode' AND target_id = ? AND watched_at = '2018-01-01T20:00:00Z'`, alice, bbS1E1); n != 1 {
			t.Errorf("S1E1 watch not stored at the UTC instant")
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history
			WHERE user_id = ? AND target_type = 'episode' AND target_id = ?`, alice, bbS2E1); n != 1 {
			t.Errorf("S2E1 watches = %d, want 1", n)
		}
		// The other show and Bob are untouched.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history WHERE user_id = ? AND target_id = ? AND target_type = 'episode'`, alice, otherEp); n != 1 {
			t.Errorf("a show not in the file lost watches")
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history WHERE user_id = ?`, bob); n != 2 {
			t.Errorf("bob's watches = %d, want 2", n)
		}
		// Ratings carry the file's ratedAt, not the clock.
		var rating int
		var updatedAt string
		err = svc.DB.ReadRow(ctx, `SELECT rating, updated_at FROM user_video_ratings
			WHERE user_id = ? AND target_type = 'show' AND target_id = ?`, alice, bbID).Scan(&rating, &updatedAt)
		if err != nil || rating != 8 || updatedAt != "2026-08-01T10:00:00Z" {
			t.Errorf("show rating = %d @ %q, %v", rating, updatedAt, err)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_watchlist WHERE user_id = ? AND target_type = 'show' AND target_id = ?`, alice, bbID); n != 1 {
			t.Errorf("watchlist row missing")
		}
		// Favorites carry the file's favoritedAt as added_at.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_favorites
			WHERE user_id = ? AND added_at = '2026-08-01T10:00:00Z'
			  AND ((target_type = 'movie' AND target_id = ?) OR (target_type = 'show' AND target_id = ?))`, alice, matrix.ID, bbID); n != 2 {
			t.Errorf("favorite rows = %d, want 2 at the file's instant", n)
		}
		// Status follows the log: the movie is watched, the show is watching
		// (S2E3 and S2E4 have aired and are unwatched).
		if st := videotest.MovieStatus(t, svc.DB, alice, matrix.ID); st != "watched" {
			t.Errorf("movie status = %q", st)
		}
		if st := videotest.ShowStatus(t, svc.DB, alice, bbID); st != "watching" {
			t.Errorf("show status = %q, want watching", st)
		}
	})

	t.Run("a rating-only or watchlist-only item keeps its watches", func(t *testing.T) {
		t.Parallel()
		svc, alice := restoreService(t, &videotest.ProviderStub{})
		bbID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		ep := videotest.SeedEpisode(t, svc.DB, bbID, 1, 1, videotest.Ptr("2008-01-20"))
		videotest.SeedWatch(t, svc.DB, alice, "episode", ep, "2020-01-01T00:00:00Z")
		matrix := videotest.SeedMovie(t, svc.DB, 603, "The Matrix", nil, nil, `{"external_ids": {}, "images": {}}`)
		videotest.SeedWatch(t, svc.DB, alice, "movie", matrix.ID, "2020-01-01T00:00:00Z")

		job := runRestore(t, svc, alice, tbDump(nil,
			[]TicketBoxRatingEntry{tbRating("show", 1396, 8, at)},
			[]TicketBoxWatchlistEntry{tbWatchlist("movie", 603, at)}))
		if job.State != "done" || job.Report.HistoryReplaced != 0 || job.Report.HistoryInserted != 0 {
			t.Fatalf("job = %+v", job)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history WHERE user_id = ?`, alice); n != 2 {
			t.Errorf("watches = %d, want 2 untouched", n)
		}
		// No recompute ran: a rating is not a watch, so no state row appeared.
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_show_states`); n != 0 {
			t.Errorf("a rating-only group wrote a show state")
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_movie_states`); n != 0 {
			t.Errorf("a watchlist-only group wrote a movie state")
		}
	})

	t.Run("a favorite-only movie is materialised and keeps no status", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, alice := restoreService(t, stub)

		job := runRestore(t, svc, alice, tbFavorites(tbDump(nil, nil, nil), tbFavorite("movie", 603, at)))
		if job.State != "done" || job.Report.MoviesMaterialised != 1 || job.Report.FavoritesInserted != 1 || job.Report.MoviesTotal != 1 {
			t.Fatalf("job = %+v", job)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_video_favorites WHERE user_id = ? AND target_type = 'movie'`, alice); n != 1 {
			t.Errorf("favorite rows = %d, want 1", n)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_movie_states`); n != 0 {
			t.Errorf("a favorite-only group wrote a movie state")
		}
	})

	t.Run("a dropped show stays dropped", func(t *testing.T) {
		t.Parallel()
		svc, alice := restoreService(t, &videotest.ProviderStub{})
		bbID := videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		videotest.SeedEpisode(t, svc.DB, bbID, 1, 1, videotest.Ptr("2008-01-20"))
		videotest.SeedShowStatus(t, svc.DB, alice, bbID, "dropped")

		job := runRestore(t, svc, alice, tbDump([]TicketBoxHistoryEntry{tbEpisodeWatch(1396, 1, 1, at)}, nil, nil))
		if job.State != "done" || job.Report.HistoryInserted != 1 {
			t.Fatalf("job = %+v", job)
		}
		if st := videotest.ShowStatus(t, svc.DB, alice, bbID); st != "dropped" {
			t.Errorf("status = %q, want dropped", st)
		}
	})

	t.Run("an episode TMDB does not list is skipped, the rest of the show lands", func(t *testing.T) {
		t.Parallel()
		svc, alice := restoreService(t, &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": videotest.ShowWithSeasonBody}})

		job := runRestore(t, svc, alice, tbDump(
			[]TicketBoxHistoryEntry{tbEpisodeWatch(1396, 9, 9, at), tbEpisodeWatch(1396, 2, 1, at), tbEpisodeWatch(1396, 2, 9, at)},
			[]TicketBoxRatingEntry{tbEpisodeRating(1396, 9, 9, 5, at), tbEpisodeRating(1396, 2, 1, 7, at)}, nil))
		if job.State != "done" {
			t.Fatalf("job = %+v", job)
		}
		r := job.Report
		if r.ShowsMaterialised != 1 || r.HistoryInserted != 1 || r.RatingsUpserted != 1 || r.SkippedTotal != 3 {
			t.Errorf("report = %+v", r)
		}
		if r.SkippedByReason[common.SkipEpisodeNotFound] != 3 || len(r.Skipped) != 3 || *r.Skipped[0].Season != 9 {
			t.Fatalf("skipped = %+v", r.Skipped)
		}
		// Each skip names the show and says why: season 9 does not exist at
		// all; season 2 ends at episode 4, so 2x9 is past its end.
		for _, sk := range r.Skipped {
			if sk.Title == nil || *sk.Title != "Breaking Bad" || sk.Hint == nil {
				t.Errorf("skip %+v: want title Breaking Bad and a hint", sk)
				continue
			}
			want, wantLast := hintSeasonMissing, 0
			if *sk.Season == 2 {
				want, wantLast = hintPastSeasonEnd, 4
			}
			if *sk.Hint != want || (wantLast == 0) != (sk.TMDBLastEpisode == nil) ||
				(sk.TMDBLastEpisode != nil && *sk.TMDBLastEpisode != wantLast) {
				t.Errorf("S%dE%d hint = %v / last %v, want %s / %d", *sk.Season, *sk.Episode, *sk.Hint, sk.TMDBLastEpisode, want, wantLast)
			}
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM shows WHERE tmdb_id = 1396`); n != 1 {
			t.Errorf("show not materialised")
		}
	})

	t.Run("an episode is found by its TMDB id first, by position as the fallback", func(t *testing.T) {
		t.Parallel()
		// TMDB's ids for S2E3 and S2E4 — the shared fixture's episodes carry
		// none, the way a renumbered special's old position would not match.
		body := strings.Replace(videotest.ShowWithSeasonBody, `{"episode_number": 3, "name": "Break-In"`,
			`{"id": 62094, "episode_number": 3, "name": "Break-In"`, 1)
		body = strings.Replace(body, `{"episode_number": 4, "name": "ABQ"`,
			`{"id": 62095, "episode_number": 4, "name": "ABQ"`, 1)
		svc, alice := restoreService(t, &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": body}})

		// Another show whose season document names TMDB id 7777: an id is
		// only ever resolved within the show being imported.
		otherID := videotest.SeedShow(t, svc.DB, 999, "Other Show")
		videotest.SeedSeasonSnapshot(t, svc.DB, otherID, 999, 1, nil, `{"episodes": [{"id": 7777, "episode_number": 1}]}`)
		otherEp := videotest.SeedEpisode(t, svc.DB, otherID, 1, 1, videotest.Ptr("2010-01-01"))

		withID := func(e TicketBoxHistoryEntry, id *int64) TicketBoxHistoryEntry {
			e.TMDBID = id
			return e
		}
		renumbered := tbEpisodeRating(1396, 0, 401, 9, at) // TMDB moved it to S2E4
		renumbered.TMDBID = videotest.Ptr(int64(62095))
		job := runRestore(t, svc, alice, tbDump(
			[]TicketBoxHistoryEntry{
				withID(tbEpisodeWatch(1396, 0, 401, at), videotest.Ptr(int64(62094))), // stale position, known id → S2E3
				withID(tbEpisodeWatch(1396, 2, 1, at), nil),                           // no id → position S2E1
				withID(tbEpisodeWatch(1396, 2, 4, at), videotest.Ptr(int64(7777))),    // another show's id → position S2E4
				withID(tbEpisodeWatch(1396, 9, 9, at), videotest.Ptr(int64(123456))),  // neither resolves → skipped
			},
			[]TicketBoxRatingEntry{renumbered}, nil))
		if job.State != "done" {
			t.Fatalf("job = %+v", job)
		}
		r := job.Report
		if r.HistoryInserted != 3 || r.RatingsUpserted != 1 || r.SkippedTotal != 1 || r.SkippedByReason[common.SkipEpisodeNotFound] != 1 {
			t.Errorf("report = %+v", r)
		}
		if len(r.Skipped) != 1 || *r.Skipped[0].Season != 9 || *r.Skipped[0].Episode != 9 {
			t.Errorf("skipped = %+v, want the file's own position 9x9", r.Skipped)
		}
		watched := func(season, episode int) int {
			return videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history w
				JOIN episodes e ON w.target_type = 'episode' AND e.id = w.target_id
				JOIN shows s ON s.id = e.show_id
				WHERE w.user_id = ? AND s.tmdb_id = 1396 AND e.season_number = ? AND e.episode_number = ?`,
				alice, season, episode)
		}
		for _, k := range []episodeKey{{2, 3}, {2, 1}, {2, 4}} {
			if n := watched(k.Season, k.Episode); n != 1 {
				t.Errorf("S%dE%d watches = %d, want 1", k.Season, k.Episode, n)
			}
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history WHERE user_id = ? AND target_type = 'episode' AND target_id = ?`, alice, otherEp); n != 0 {
			t.Errorf("an id from another show reached it: %d watches", n)
		}
		var rated int
		err := svc.DB.ReadRow(ctx, `SELECT vr.rating FROM user_video_ratings vr
			JOIN episodes e ON vr.target_type = 'episode' AND e.id = vr.target_id
			JOIN shows s ON s.id = e.show_id
			WHERE vr.user_id = ? AND s.tmdb_id = 1396 AND e.season_number = 2 AND e.episode_number = 4`, alice).Scan(&rated)
		if err != nil || rated != 9 {
			t.Errorf("S2E4 rating = %d, %v; want 9 resolved by id", rated, err)
		}
	})

	t.Run("skips are named from the catalog when the job finishes", func(t *testing.T) {
		t.Parallel()
		svc, alice := restoreService(t, &videotest.ProviderStub{})
		videotest.SeedShow(t, svc.DB, 1396, "Breaking Bad")
		videotest.SeedMovie(t, svc.DB, 603, "The Matrix", nil, nil, `{"external_ids": {}, "images": {}}`)

		seasonListed := TicketBoxWatchlistEntry{TicketBoxAddress: TicketBoxAddress{Type: "season",
			TMDBID: videotest.Ptr(int64(3572)), ShowTMDBID: videotest.Ptr(int64(1396)), Season: videotest.Ptr(2)}, ListedAt: at}
		unknownShow := TicketBoxRatingEntry{TicketBoxAddress: TicketBoxAddress{Type: "season",
			ShowTMDBID: videotest.Ptr(int64(4242)), Season: videotest.Ptr(1)}, Rating: videotest.Ptr(7), RatedAt: at}
		job := runRestore(t, svc, alice, tbDump(
			[]TicketBoxHistoryEntry{tbMovieWatch(603, "2099-01-01T00:00:00Z")},
			[]TicketBoxRatingEntry{unknownShow},
			[]TicketBoxWatchlistEntry{seasonListed}))
		if job.State != "done" || job.Report.SkippedTotal != 3 {
			t.Fatalf("job = %+v", job)
		}
		titles := map[string]*string{}
		for _, sk := range job.Report.Skipped {
			titles[sk.Kind] = sk.Title
			if sk.Hint != nil {
				t.Errorf("a %s skip carries a hint: %+v", sk.Reason, sk)
			}
		}
		if got := titles["history"]; got == nil || *got != "The Matrix" {
			t.Errorf("movie skip title = %v, want The Matrix", got)
		}
		if got := titles["watchlist"]; got == nil || *got != "Breaking Bad" {
			t.Errorf("season skip title = %v, want the show's", got)
		}
		if got := titles["rating"]; got != nil {
			t.Errorf("unknown show's title = %q, want null", *got)
		}
	})

	t.Run("a show TMDB does not know skips its whole group and the run goes on", func(t *testing.T) {
		t.Parallel()
		svc, alice := restoreService(t, &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}})

		job := runRestore(t, svc, alice, tbFavorites(tbDump(
			[]TicketBoxHistoryEntry{tbEpisodeWatch(4040, 1, 1, at), tbMovieWatch(603, at)},
			[]TicketBoxRatingEntry{tbRating("show", 4040, 5, at)},
			[]TicketBoxWatchlistEntry{tbWatchlist("show", 4040, at)}), tbFavorite("show", 4040, at)))
		if job.State != "done" {
			t.Fatalf("job = %+v", job)
		}
		r := job.Report
		if r.MoviesMaterialised != 1 || r.HistoryInserted != 1 || r.SkippedTotal != 4 || r.SkippedByReason[common.SkipShowNotFound] != 4 {
			t.Errorf("report = %+v", r)
		}
		kinds := map[string]int{}
		for _, sk := range r.Skipped {
			kinds[sk.Kind]++
		}
		if kinds["history"] != 1 || kinds["rating"] != 1 || kinds["watchlist"] != 1 || kinds["favorite"] != 1 {
			t.Errorf("skipped kinds = %v", kinds)
		}
	})

	t.Run("an outage skips the group, and three in a row stop the run asking", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{
			Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody},
			Errs: map[string]error{
				"/3/movie/1": videotest.ErrTimeout, "/3/movie/2": videotest.ErrTimeout, "/3/movie/3": videotest.ErrTimeout,
				"/3/tv/1396": videotest.ErrTimeout,
			},
		}
		svc, alice := restoreService(t, stub)
		// Movie 603 is already local, so it needs no fetch and must still land
		// after the breaker trips; it sorts after 1, 2 and 3.
		videotest.SeedMovie(t, svc.DB, 603, "The Matrix", nil, nil, `{"external_ids": {}, "images": {}}`)

		job := runRestore(t, svc, alice, tbDump(
			[]TicketBoxHistoryEntry{
				tbMovieWatch(1, at), tbMovieWatch(2, at), tbMovieWatch(3, at),
				tbMovieWatch(603, at), tbMovieWatch(700, at),
				tbEpisodeWatch(1396, 1, 1, at),
			}, nil, nil))
		if job.State != "done" {
			t.Fatalf("job = %+v", job)
		}
		r := job.Report
		if r.SkippedByReason[common.SkipMovieUnavailable] != 3 {
			t.Errorf("unavailable = %d, want 3: %+v", r.SkippedByReason[common.SkipMovieUnavailable], r)
		}
		if r.SkippedByReason[common.SkipProviderUnavailable] != 2 {
			t.Errorf("provider_unavailable = %d, want 2 (movie 700 and the show): %+v", r.SkippedByReason[common.SkipProviderUnavailable], r)
		}
		if r.HistoryInserted != 1 {
			t.Errorf("history_inserted = %d, want 1 (the local movie)", r.HistoryInserted)
		}
		if stub.CallCount("/3/movie/700") != 0 || stub.CallCount("/3/tv/1396") != 0 {
			t.Errorf("the open breaker still called TMDB: %v", stub.Calls)
		}
	})

	t.Run("running the same file twice converges", func(t *testing.T) {
		t.Parallel()
		svc, alice := restoreService(t, &videotest.ProviderStub{Bodies: map[string]string{
			"/3/movie/603": videotest.MatrixDetailsBody, "/3/tv/1396": videotest.ShowWithSeasonBody}})
		dump := tbFavorites(tbDump(
			[]TicketBoxHistoryEntry{tbMovieWatch(603, at), tbMovieWatch(603, "2020-01-01T00:00:00Z"), tbEpisodeWatch(1396, 2, 1, at)},
			[]TicketBoxRatingEntry{tbRating("movie", 603, 9, at)},
			[]TicketBoxWatchlistEntry{tbWatchlist("movie", 603, at)}), tbFavorite("show", 1396, at))

		first := runRestore(t, svc, alice, dump)
		second := runRestore(t, svc, alice, dump)
		if first.Report.HistoryInserted != 3 || second.Report.HistoryInserted != 3 || second.Report.HistoryReplaced != 3 {
			t.Errorf("first = %+v, second = %+v", first.Report, second.Report)
		}
		if first.Report.FavoritesInserted != 1 {
			t.Errorf("first run favorites = %d, want 1", first.Report.FavoritesInserted)
		}
		if second.Report.MoviesMaterialised != 0 || second.Report.ShowsMaterialised != 0 || second.Report.WatchlistInserted != 0 || second.Report.FavoritesInserted != 0 {
			t.Errorf("second run re-materialised or re-listed: %+v", second.Report)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history WHERE user_id = ?`, alice); n != 3 {
			t.Errorf("watches after two runs = %d, want 3", n)
		}
	})

	t.Run("the skipped list is capped, the counts are not", func(t *testing.T) {
		t.Parallel()
		ratings := make([]TicketBoxRatingEntry, 0, 250)
		for i := 0; i < 250; i++ {
			ratings = append(ratings, tbRating("season", int64(i+1), 5, at))
		}
		plan, skips := ValidateTicketBoxDump(tbDump(nil, ratings, nil), time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC))
		job := NewRestoreJob(time.Now(), plan, skips)
		snap := job.Snapshot()
		if snap.Report.SkippedTotal != 250 || len(snap.Report.Skipped) != restoreSkipCap || snap.Report.SkippedByReason[skipSeasonRating] != 250 {
			t.Errorf("total %d, listed %d, by reason %v", snap.Report.SkippedTotal, len(snap.Report.Skipped), snap.Report.SkippedByReason)
		}
	})
}

// waitFor polls the user's job until it leaves running or the deadline passes.
func waitFor(t *testing.T, svc *Service, userID int64) RestoreJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		job, err := svc.TicketBoxRestoreStatus(userID)
		if err != nil {
			t.Fatalf("TicketBoxRestoreStatus: %v", err)
		}
		if job.State != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job still running: %+v", job)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRestoreRunner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	movieDump := func(id int64) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"schemaVersion": 5, "exportedAt": "x", "history": [
			{"type": "movie", "tmdbId": %d, "addedAt": "2026-08-01T10:00:00.000Z", "historyType": "watch"}]}`, id))
	}

	t.Run("status before any start is a 404", func(t *testing.T) {
		t.Parallel()
		svc, alice := restoreService(t, &videotest.ProviderStub{})
		if _, err := svc.TicketBoxRestoreStatus(alice); !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("a second start while one runs is a 409; after it finishes a new one is accepted", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		gate := &videotest.GatedDoer{Inner: stub, Gate: make(chan struct{})}
		svc, _ := detailsService(t, stub)
		svc.TMDB = videotest.NewGatedTMDB(gate)
		alice := videotest.SeedUser(t, svc.DB, "alice")

		first, err := svc.StartTicketBoxRestore(ctx, alice, movieDump(603))
		if err != nil || first.State != "running" || first.Progress.GroupsTotal != 1 {
			t.Fatalf("first start = %+v, %v", first, err)
		}
		if _, err := svc.StartTicketBoxRestore(ctx, alice, movieDump(603)); !errors.Is(err, httpx.ErrConflict) {
			t.Fatalf("second start err = %v, want ErrConflict", err)
		}
		// Another user is not blocked by Alice's job.
		bob := videotest.SeedUser(t, svc.DB, "bob")
		if _, err := svc.StartTicketBoxRestore(ctx, bob, json.RawMessage(`{"schemaVersion": 5}`)); err != nil {
			t.Fatalf("bob's start: %v", err)
		}

		close(gate.Gate)
		done := waitFor(t, svc, alice)
		if done.State != "done" || done.Report.MoviesMaterialised != 1 || done.Report.HistoryInserted != 1 {
			t.Errorf("finished job = %+v", done)
		}
		if _, err := svc.StartTicketBoxRestore(ctx, alice, movieDump(603)); err != nil {
			t.Errorf("start after done: %v", err)
		}
		waitFor(t, svc, alice)
	})

	t.Run("a request context ending does not end the job", func(t *testing.T) {
		t.Parallel()
		svc, alice := restoreService(t, &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}})
		reqCtx, cancel := context.WithCancel(ctx)
		if _, err := svc.StartTicketBoxRestore(reqCtx, alice, movieDump(603)); err != nil {
			t.Fatalf("start: %v", err)
		}
		cancel()
		if job := waitFor(t, svc, alice); job.State != "done" {
			t.Errorf("job = %+v, want done despite the request ending", job)
		}
	})

	t.Run("shutdown cancels a running job, which fails as cancelled", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		gate := &videotest.GatedDoer{Inner: stub, Gate: make(chan struct{})}
		svc, _ := detailsService(t, stub)
		svc.TMDB = videotest.NewGatedTMDB(gate)
		alice := videotest.SeedUser(t, svc.DB, "alice")
		if _, err := svc.StartTicketBoxRestore(ctx, alice, movieDump(603)); err != nil {
			t.Fatalf("start: %v", err)
		}

		shutdown, stop := context.WithCancel(ctx)
		stop()
		svc.Run(shutdown) // returns once the job has stopped

		job, err := svc.TicketBoxRestoreStatus(alice)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if job.State != "failed" || job.Error == nil || job.Error.Code != "cancelled" {
			t.Errorf("job = %+v, want failed/cancelled", job)
		}
		if n := videotest.CountRows(t, svc.DB, `SELECT COUNT(*) FROM user_watch_history`); n != 0 {
			t.Errorf("a cancelled group left %d rows", n)
		}
	})
}
