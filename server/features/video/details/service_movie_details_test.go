package details

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"mbyte.ro/sofalog/assets"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
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

func TestMovieDetails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("an untracked view is served live, stores nothing and maps the DTO", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.ID != nil || d.TMDBID != 603 || d.Title != "The Matrix" {
			t.Errorf("identity = %+v, want a null id (no catalog row)", d)
		}
		if d.Tracked || d.Watched || d.Watchlisted {
			t.Errorf("flags = %v/%v/%v, want all false on a live view", d.Tracked, d.Watched, d.Watchlisted)
		}
		// Viewing never creates the catalog row — only tracking does.
		if _, err := common.GetMovieByTMDBID(ctx, svc.DB, 603); !errors.Is(err, httpx.ErrNotFound) {
			t.Errorf("a live view materialised a movies row: err = %v, want ErrNotFound", err)
		}
		if d.Year == nil || *d.Year != "1999" {
			t.Errorf("year = %v, want 1999", d.Year)
		}
		// Similar: the appended recommendations, TMDB's order kept, the id-less
		// and title-less entries dropped, missing date/poster/score null.
		if len(d.Similar) != 2 || d.Similar[0].TMDBID != 604 || d.Similar[1].TMDBID != 606 {
			t.Fatalf("similar = %+v, want Reloaded then the untitled sequel", d.Similar)
		}
		if d.Similar[0].Year == nil || *d.Similar[0].Year != "2003" ||
			d.Similar[0].Rating == nil || *d.Similar[0].Rating != 7.0 ||
			d.Similar[0].Poster == nil || *d.Similar[0].Poster != "api/v1/video/posters/w185/r.jpg" ||
			d.Similar[0].PosterFull == nil || *d.Similar[0].PosterFull != "api/v1/video/posters/w500/r.jpg" {
			t.Errorf("similar[0] = %+v, want year 2003, rating 7.0 and the two proxy posters", d.Similar[0])
		}
		if d.Similar[1].Year != nil || d.Similar[1].Rating != nil || d.Similar[1].Poster != nil {
			t.Errorf("similar[1] = %+v, want null year, rating and poster", d.Similar[1])
		}
		if d.RuntimeMin == nil || *d.RuntimeMin != 136 {
			t.Errorf("runtime_min = %v, want 136", d.RuntimeMin)
		}
		if d.Tagline == nil || *d.Tagline != "Free your mind." {
			t.Errorf("tagline = %v", d.Tagline)
		}
		if len(d.Genres) != 2 || d.Genres[0] != "Action" {
			t.Errorf("genres = %v", d.Genres)
		}
		if d.Rating == nil || *d.Rating != 8.2 || d.Votes == nil || *d.Votes != 26000 {
			t.Errorf("rating = %v votes = %v", d.Rating, d.Votes)
		}
		if d.Director == nil || *d.Director != "Lana Wachowski" {
			t.Errorf("director = %v", d.Director)
		}
		// Cast is ordered by TMDB's order field, not response order.
		if len(d.Cast) != 2 || d.Cast[0].Name != "Keanu Reeves" || d.Cast[1].Name != "Carrie-Anne Moss" {
			t.Errorf("cast = %+v", d.Cast)
		}
		if d.Cast[0].Photo == nil || *d.Cast[0].Photo != "api/v1/video/posters/w185/n.jpg" {
			t.Errorf("cast photo = %v", d.Cast[0].Photo)
		}
		// The person id is what makes a cast card a link (D-027); a body
		// without one yields null, never 0.
		if d.Cast[0].TMDBID == nil || *d.Cast[0].TMDBID != 6384 {
			t.Errorf("cast[0].tmdb_id = %v, want 6384", d.Cast[0].TMDBID)
		}
		if d.Cast[1].TMDBID != nil {
			t.Errorf("cast[1].tmdb_id = %v, want null for an entry without an id", *d.Cast[1].TMDBID)
		}
		if d.Poster == nil || *d.Poster != "api/v1/video/posters/w185/m.jpg" {
			t.Errorf("poster = %v", d.Poster)
		}
		if d.PosterFull == nil || *d.PosterFull != "api/v1/video/posters/w500/m.jpg" {
			t.Errorf("poster_full = %v", d.PosterFull)
		}
		if d.Homepage == nil || *d.Homepage != "https://thematrix.example" {
			t.Errorf("homepage = %v", d.Homepage)
		}
		if d.IMDBID == nil || *d.IMDBID != "tt0133093" {
			t.Errorf("imdb_id = %v", d.IMDBID)
		}
		// The official YouTube trailer wins over the teaser (lower type), the
		// fan cut (unofficial, even though newer) and the Vimeo entry.
		if d.TrailerURL == nil || *d.TrailerURL != "https://www.youtube.com/watch?v=vKQi3bBA1y8" {
			t.Errorf("trailer_url = %v", d.TrailerURL)
		}
		if d.OriginalLanguage == nil || *d.OriginalLanguage != "en" {
			t.Errorf("original_language = %v", d.OriginalLanguage)
		}
		if d.Budget == nil || *d.Budget != 63000000 || d.Revenue == nil || *d.Revenue != 463517383 {
			t.Errorf("budget = %v revenue = %v", d.Budget, d.Revenue)
		}
		if d.Popularity == nil || *d.Popularity != 85.3 {
			t.Errorf("popularity = %v", d.Popularity)
		}
		if stub.CallCount("/3/movie/603") != 1 {
			t.Errorf("tmdb calls = %d, want 1", stub.CallCount("/3/movie/603"))
		}

		// With no row to answer from, every untracked view is live again.
		again, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("second MovieDetails: %v", err)
		}
		if again.ID != nil {
			t.Errorf("second live view returned id %v, want null", again.ID)
		}
		if stub.CallCount("/3/movie/603") != 2 {
			t.Errorf("tmdb calls after second view = %d, want 2 (live each time)", stub.CallCount("/3/movie/603"))
		}
	})

	t.Run("a catalog row answers views without TMDB, by tmdb_id and by id", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)

		// Materialise the row the way tracking does.
		row, err := svc.RefreshMovie(ctx, 603)
		if err != nil {
			t.Fatalf("refreshMovie: %v", err)
		}

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.ID == nil || *d.ID != row.ID {
			t.Errorf("id = %v, want the catalog id %d", d.ID, row.ID)
		}
		byID, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{ID: fmt.Sprint(row.ID)})
		if err != nil {
			t.Fatalf("MovieDetails by id: %v", err)
		}
		if byID.TMDBID != 603 {
			t.Errorf("by-id lookup returned tmdb_id %d", byID.TMDBID)
		}
		// Both views were answered from the row: only RefreshMovie hit TMDB.
		if stub.CallCount("/3/movie/603") != 1 {
			t.Errorf("tmdb calls = %d, want 1", stub.CallCount("/3/movie/603"))
		}
	})

	t.Run("a stale snapshot refreshes; a fresh one does not", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, clock := detailsService(t, stub)

		row, err := svc.RefreshMovie(ctx, 603)
		if err != nil {
			t.Fatalf("refreshMovie: %v", err)
		}
		first := common.MovieDetailsFromRow(row, nil, videotest.WatchRegion)

		// 47h later: still fresh.
		*clock = clock.Add(47 * time.Hour)
		if _, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"}); err != nil {
			t.Fatalf("MovieDetails at 47h: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 1 {
			t.Errorf("tmdb calls at 47h = %d, want 1", stub.CallCount("/3/movie/603"))
		}

		// Past 48h: stale, refreshed, fetched_at rewritten.
		*clock = clock.Add(2 * time.Hour)
		refreshed, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails at 49h: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 2 {
			t.Errorf("tmdb calls at 49h = %d, want 2", stub.CallCount("/3/movie/603"))
		}
		if refreshed.FetchedAt == first.FetchedAt {
			t.Errorf("fetched_at not rewritten on refresh: %s", refreshed.FetchedAt)
		}
		if refreshed.ID == nil || first.ID == nil || *refreshed.ID != *first.ID {
			t.Errorf("refresh changed the local id: %v then %v", first.ID, refreshed.ID)
		}
	})

	t.Run("a snapshot predating external_ids refreshes once, even when fresh", func(t *testing.T) {
		t.Parallel()
		// The materialised snapshot lacks external_ids/videos — the shape
		// every row had before those sections were appended to the TMDB call.
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/movie/603": `{"id": 603, "title": "The Matrix"}`,
		}}
		svc, _ := detailsService(t, stub)

		if _, err := svc.RefreshMovie(ctx, 603); err != nil {
			t.Fatalf("refreshMovie: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 1 {
			t.Fatalf("tmdb calls after materialising = %d, want 1", stub.CallCount("/3/movie/603"))
		}

		// The next view notices the missing sections and refreshes despite the
		// snapshot being minutes old; the refetched body now carries them.
		stub.Mu.Lock()
		stub.Bodies["/3/movie/603"] = videotest.MatrixDetailsBody
		stub.Mu.Unlock()

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("second MovieDetails: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 2 {
			t.Errorf("tmdb calls after second view = %d, want 2", stub.CallCount("/3/movie/603"))
		}
		if d.IMDBID == nil || *d.IMDBID != "tt0133093" {
			t.Errorf("imdb_id after refresh = %v", d.IMDBID)
		}

		// Once the key exists the probe never triggers again.
		if _, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"}); err != nil {
			t.Fatalf("third MovieDetails: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 2 {
			t.Errorf("tmdb calls after third view = %d, want still 2", stub.CallCount("/3/movie/603"))
		}
	})

	t.Run("a failed refresh serves the stale snapshot", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, clock := detailsService(t, stub)

		if _, err := svc.RefreshMovie(ctx, 603); err != nil {
			t.Fatalf("refreshMovie: %v", err)
		}

		// Provider goes down, snapshot goes stale: the page still renders.
		stub.Mu.Lock()
		stub.Errs = map[string]error{"/3/movie/603": errors.New("dial tcp: timeout")}
		stub.Mu.Unlock()
		*clock = clock.Add(72 * time.Hour)

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("stale + provider down must serve the snapshot, got: %v", err)
		}
		if d.Title != "The Matrix" {
			t.Errorf("served %+v", d)
		}
	})

	t.Run("an id miss is 404 and never touches TMDB", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{}
		svc, _ := detailsService(t, stub)

		_, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{ID: "42"})
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		if n := stub.CallCount("/3/movie/42"); n != 0 {
			t.Errorf("an id lookup reached TMDB %d times", n)
		}
	})

	t.Run("an unknown tmdb_id is 404", func(t *testing.T) {
		t.Parallel()
		// Unscripted stub paths return 404, which is exactly TMDB's answer
		// for a movie that does not exist.
		svc, _ := detailsService(t, &videotest.ProviderStub{})

		_, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "999999999"})
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("the full cast is returned, not a top-billed slice", func(t *testing.T) {
		t.Parallel()
		// 14 members, listed in reverse billing order so ordering is exercised
		// past the old 10-entry truncation point too.
		members := make([]string, 0, 14)
		for i := 13; i >= 0; i-- {
			members = append(members,
				fmt.Sprintf(`{"name": "Actor %02d", "character": "Role %02d", "order": %d}`, i, i, i))
		}
		body := fmt.Sprintf(
			`{"id": 900, "title": "Ensemble", "credits": {"cast": [%s]}}`,
			strings.Join(members, ","))
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/900": body}}
		svc, _ := detailsService(t, stub)

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "900"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if len(d.Cast) != 14 {
			t.Fatalf("cast length = %d, want 14 (all credited members)", len(d.Cast))
		}
		for i, cm := range d.Cast {
			if want := fmt.Sprintf("Actor %02d", i); cm.Name != want {
				t.Errorf("cast[%d] = %q, want %q", i, cm.Name, want)
			}
		}
	})

	t.Run("a snapshot without credits renders with empty credit fields", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/movie/777": `{"id": 777, "title": "Bare", "vote_average": 0, "vote_count": 0,
				"homepage": "", "budget": 0, "revenue": 0, "popularity": 0,
				"external_ids": {"imdb_id": ""}, "videos": {"results": []},
				"images": {"backdrops": [], "posters": []}}`,
		}}
		svc, _ := detailsService(t, stub)

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "777"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if d.Director != nil || len(d.Cast) != 0 || d.Cast == nil {
			t.Errorf("credits = %v / %+v, want null director and empty cast", d.Director, d.Cast)
		}
		if d.Rating != nil || d.Votes != nil {
			t.Errorf("rating = %v votes = %v, want null (0 means unrated)", d.Rating, d.Votes)
		}
		if d.Homepage != nil || d.IMDBID != nil || d.TrailerURL != nil {
			t.Errorf("links = %v / %v / %v, want all null", d.Homepage, d.IMDBID, d.TrailerURL)
		}
		if d.Budget != nil || d.Revenue != nil || d.Popularity != nil {
			t.Errorf("facts = %v / %v / %v, want all null (0 means unknown)", d.Budget, d.Revenue, d.Popularity)
		}
		if d.Year != nil || d.RuntimeMin != nil || d.Tagline != nil || d.Poster != nil {
			t.Errorf("optional fields not null: %+v", d)
		}
		// Empty, never null — the SPA branches on length, not on presence.
		if d.Videos == nil || len(d.Videos) != 0 ||
			d.Backdrops == nil || len(d.Backdrops) != 0 ||
			d.Posters == nil || len(d.Posters) != 0 {
			t.Errorf("media = %+v / %+v / %+v, want three empty arrays", d.Videos, d.Backdrops, d.Posters)
		}
		if d.Genres == nil || len(d.Genres) != 0 {
			t.Errorf("genres = %v, want empty array", d.Genres)
		}
	})

	t.Run("the media section carries every video type, best trailer first", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}

		// Every YouTube video survives, not just trailers and teasers — but the
		// order matches trailer_url's ranking, so the tab opens on the video the
		// Trailer link plays. The Vimeo entry is dropped: no thumbnail source and
		// no watch URL the proxy could serve.
		wantKeys := []string{"vKQi3bBA1y8", "fan-cut", "aBcDeFgHiJk", "m8e-FF8MsqU"}
		if len(d.Videos) != len(wantKeys) {
			t.Fatalf("videos = %d entries (%+v), want %d", len(d.Videos), d.Videos, len(wantKeys))
		}
		for i, key := range wantKeys {
			if want := "https://www.youtube.com/watch?v=" + key; d.Videos[i].URL != want {
				t.Errorf("videos[%d].url = %s, want %s", i, d.Videos[i].URL, want)
			}
		}
		if d.Videos[0].Name != "Official Trailer" || d.Videos[0].Type != "Trailer" || !d.Videos[0].Official {
			t.Errorf("videos[0] = %+v", d.Videos[0])
		}
		if d.Videos[0].Thumb == nil || *d.Videos[0].Thumb != "api/v1/video/youtube-thumbs/vKQi3bBA1y8" {
			t.Errorf("videos[0].thumb = %v", d.Videos[0].Thumb)
		}
		if d.Videos[0].PublishedAt == nil || *d.Videos[0].PublishedAt != "1999-02-01T00:00:00.000Z" {
			t.Errorf("videos[0].published_at = %v", d.Videos[0].PublishedAt)
		}
		// "fan-cut" is not a YouTube id, so it gets no thumb rather than a URL
		// the proxy would reject — the SPA renders a placeholder tile for null.
		if d.Videos[1].Thumb != nil {
			t.Errorf("videos[1].thumb = %v, want null for an unusable key", *d.Videos[1].Thumb)
		}
	})

	t.Run("galleries are ranked, deduplicated of empty paths and sized per kind", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}

		// Best-rated first; the 5.1/3 tie breaks on file path, so the order is
		// stable across refreshes rather than TMDB's response order. The entry
		// with an empty file_path is dropped — it addresses no image.
		wantBackdrops := []string{"/best.jpg", "/low.jpg", "/tie-b.jpg"}
		if len(d.Backdrops) != len(wantBackdrops) {
			t.Fatalf("backdrops = %+v, want %d entries", d.Backdrops, len(wantBackdrops))
		}
		for i, file := range wantBackdrops {
			if want := "api/v1/video/posters/w300" + file; d.Backdrops[i].Thumb != want {
				t.Errorf("backdrops[%d].thumb = %s, want %s", i, d.Backdrops[i].Thumb, want)
			}
			if want := "api/v1/video/posters/w1280" + file; d.Backdrops[i].Full != want {
				t.Errorf("backdrops[%d].full = %s, want %s", i, d.Backdrops[i].Full, want)
			}
		}
		if d.Backdrops[0].Width != 3840 || d.Backdrops[0].Height != 2160 {
			t.Errorf("backdrops[0] dimensions = %dx%d", d.Backdrops[0].Width, d.Backdrops[0].Height)
		}

		// Posters use the poster ladder, not the backdrop one.
		if len(d.Posters) != 2 {
			t.Fatalf("posters = %+v, want 2 entries", d.Posters)
		}
		if d.Posters[0].Thumb != "api/v1/video/posters/w185/p-top.jpg" ||
			d.Posters[0].Full != "api/v1/video/posters/w500/p-top.jpg" {
			t.Errorf("posters[0] = %+v", d.Posters[0])
		}
	})

	t.Run("a video list longer than the cap is truncated, best-ranked kept", func(t *testing.T) {
		t.Parallel()
		// 35 YouTube clips, published newest-first by index, so within the one
		// rank the order is clip-00 down to clip-34.
		entries := make([]string, 0, 35)
		for i := 0; i < 35; i++ {
			entries = append(entries, fmt.Sprintf(
				`{"key": "clipclipc%02d", "name": "Clip %02d", "site": "YouTube", "type": "Clip",
				  "official": true, "published_at": "20%02d-01-01T00:00:00.000Z"}`,
				i, i, 60-i))
		}
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/movie/43": `{"id": 43, "title": "Talky", "external_ids": {"imdb_id": ""},
				"videos": {"results": [` + strings.Join(entries, ",") + `]},
				"images": {"backdrops": [], "posters": []}}`,
		}}
		svc, _ := detailsService(t, stub)

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "43"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if len(d.Videos) != common.MediaGalleryMax {
			t.Fatalf("videos = %d, want the cap of %d", len(d.Videos), common.MediaGalleryMax)
		}
		if want := "https://www.youtube.com/watch?v=clipclipc00"; d.Videos[0].URL != want {
			t.Errorf("videos[0].url = %s, want the newest clip", d.Videos[0].URL)
		}
	})

	t.Run("a gallery longer than the cap is truncated to the best entries", func(t *testing.T) {
		t.Parallel()
		// 40 backdrops, rated so that vote_average descends with the index —
		// backdrop-0 is the best and backdrop-39 the worst.
		entries := make([]string, 0, 40)
		for i := 0; i < 40; i++ {
			entries = append(entries, fmt.Sprintf(
				`{"file_path": "/b%02d.jpg", "width": 1920, "height": 1080, "vote_average": %d, "vote_count": 1}`,
				i, 40-i))
		}
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/movie/42": `{"id": 42, "title": "Many", "external_ids": {"imdb_id": ""},
				"videos": {"results": []},
				"images": {"backdrops": [` + strings.Join(entries, ",") + `], "posters": []}}`,
		}}
		svc, _ := detailsService(t, stub)

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "42"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		if len(d.Backdrops) != common.MediaGalleryMax {
			t.Fatalf("backdrops = %d, want the cap of %d", len(d.Backdrops), common.MediaGalleryMax)
		}
		// The cap keeps the top of the ranking, not an arbitrary window.
		if d.Backdrops[0].Thumb != "api/v1/video/posters/w300/b00.jpg" {
			t.Errorf("backdrops[0] = %s, want the best-rated entry", d.Backdrops[0].Thumb)
		}
		if last := d.Backdrops[common.MediaGalleryMax-1].Thumb; last != "api/v1/video/posters/w300/b29.jpg" {
			t.Errorf("last kept backdrop = %s, want b29", last)
		}
	})

	t.Run("a snapshot predating images refreshes once, even when fresh", func(t *testing.T) {
		t.Parallel()
		// external_ids present, images absent: the shape every row had between
		// the two appends. The images probe alone must trigger the refresh.
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/movie/603": `{"id": 603, "title": "The Matrix",
				"external_ids": {"imdb_id": "tt0133093"}, "videos": {"results": []}}`,
		}}
		svc, _ := detailsService(t, stub)

		if _, err := svc.RefreshMovie(ctx, 603); err != nil {
			t.Fatalf("refreshMovie: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 1 {
			t.Fatalf("tmdb calls after materialising = %d, want 1", stub.CallCount("/3/movie/603"))
		}

		stub.Mu.Lock()
		stub.Bodies["/3/movie/603"] = videotest.MatrixDetailsBody
		stub.Mu.Unlock()

		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("second MovieDetails: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 2 {
			t.Errorf("tmdb calls after second view = %d, want 2", stub.CallCount("/3/movie/603"))
		}
		if len(d.Backdrops) == 0 {
			t.Errorf("backdrops still empty after the refresh")
		}

		// Once the key exists the probe never triggers again — otherwise every
		// view of the movie would call TMDB forever.
		if _, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"}); err != nil {
			t.Fatalf("third MovieDetails: %v", err)
		}
		if stub.CallCount("/3/movie/603") != 2 {
			t.Errorf("tmdb calls after third view = %d, want still 2", stub.CallCount("/3/movie/603"))
		}
	})

	t.Run("an undecodable snapshot renders three empty media arrays", func(t *testing.T) {
		t.Parallel()
		// Straight at the projection: no provider, no DB — a corrupt column is
		// not worth failing the page over, and empty is the honest rendering.
		d := common.MovieDetailsFromRow(common.MovieRow{
			ID: 1, TMDBID: 603, Title: "The Matrix",
			MetadataJSON: "{not json",
			FetchedAt:    "2026-08-19T12:00:00Z",
		}, nil, videotest.WatchRegion)
		if d.Videos == nil || len(d.Videos) != 0 ||
			d.Backdrops == nil || len(d.Backdrops) != 0 ||
			d.Posters == nil || len(d.Posters) != 0 {
			t.Errorf("media = %+v / %+v / %+v, want three empty arrays", d.Videos, d.Backdrops, d.Posters)
		}
	})

	invalid := []struct {
		name   string
		params MovieDetailsParams
	}{
		{"neither id", MovieDetailsParams{}},
		{"both ids", MovieDetailsParams{TMDBID: "603", ID: "1"}},
		{"non-numeric tmdb_id", MovieDetailsParams{TMDBID: "matrix"}},
		{"non-numeric id", MovieDetailsParams{ID: "matrix"}},
		{"negative tmdb_id", MovieDetailsParams{TMDBID: "-1"}},
	}
	for _, tt := range invalid {
		t.Run(tt.name+" is a validation error", func(t *testing.T) {
			t.Parallel()
			svc, _ := detailsService(t, &videotest.ProviderStub{})
			_, err := svc.MovieDetails(ctx, 0, tt.params)
			if !errors.Is(err, httpx.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}
