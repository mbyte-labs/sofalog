package details

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
)

// snapFor parses a details body the way every projection does, so these tests
// exercise the same path a stored snapshot takes.
func snapFor(t *testing.T, body string) common.MediaSnapshot {
	t.Helper()
	var snap common.MediaSnapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	return snap
}

func TestStreamingFrom(t *testing.T) {
	t.Parallel()

	t.Run("projects the configured region and nothing else", func(t *testing.T) {
		t.Parallel()
		snap := snapFor(t, videotest.MatrixDetailsBody)

		us := common.StreamingFrom(snap, "US")
		if us.Region != "US" {
			t.Errorf("region = %q, want US", us.Region)
		}
		if us.Link == nil || *us.Link != "https://watch.example/movie/603?locale=US" {
			t.Errorf("link = %v, want the US JustWatch link", us.Link)
		}
		// The DE listing has one service the US one does not; picking the
		// wrong key would surface it.
		for _, p := range us.Providers {
			if p.Name == "WOW" {
				t.Fatalf("providers = %+v, want no DE-only entry", us.Providers)
			}
		}

		de := common.StreamingFrom(snap, "DE")
		if len(de.Providers) != 1 || de.Providers[0].Name != "WOW" {
			t.Errorf("DE providers = %+v, want just WOW", de.Providers)
		}
	})

	// The three states the pointer encodes are the whole reason it is one: a
	// page that cannot tell "TMDB says nobody" from "we never looked" either
	// stays silent when it should speak, or claims an emptiness it never
	// checked.
	t.Run("a region the section covers but nobody serves is an ANSWER, not silence", func(t *testing.T) {
		t.Parallel()
		got := common.StreamingFrom(snapFor(t, videotest.MatrixDetailsBody), "RO")
		if got == nil {
			t.Fatalf("streaming = nil, want a non-nil empty listing — the section is there")
		}
		if got.Region != "RO" {
			t.Errorf("region = %q, want RO — an empty list must still say where", got.Region)
		}
		if got.Providers == nil || len(got.Providers) != 0 {
			t.Errorf("providers = %+v, want an empty array", got.Providers)
		}
		if got.Link != nil {
			t.Errorf("link = %v, want null", got.Link)
		}
	})

	t.Run("a snapshot predating the appended section is silence", func(t *testing.T) {
		t.Parallel()
		if got := common.StreamingFrom(snapFor(t, `{"title": "Old"}`), "US"); got != nil {
			t.Errorf("streaming = %+v, want nil — there is no listing to report on", got)
		}
	})

	t.Run("a snapshot pruned to another country is silence, not an empty answer", func(t *testing.T) {
		t.Parallel()
		// Reachable when the region changed and the refresh failed. Reporting
		// "nothing in RO" off a body that only holds US data would be a lie.
		pruned := common.PruneWatchProviders(json.RawMessage(videotest.MatrixDetailsBody), "US")
		if got := common.StreamingFrom(snapFor(t, string(pruned)), "RO"); got != nil {
			t.Errorf("streaming = %+v, want nil", got)
		}
		if got := common.StreamingFrom(snapFor(t, string(pruned)), "US"); got == nil {
			t.Fatalf("streaming = nil for the country it WAS pruned to")
		}
	})

	t.Run("a service in two buckets is one tile, at its strongest offer", func(t *testing.T) {
		t.Parallel()
		got := common.StreamingFrom(snapFor(t, videotest.MatrixDetailsBody), "US")
		if got == nil {
			t.Fatal("streaming = nil")
		}

		var appleTV []common.StreamingProvider
		for _, p := range got.Providers {
			if p.ProviderID == 2 {
				appleTV = append(appleTV, p)
			}
		}
		// Apple TV is under both rent and buy in the fixture.
		if len(appleTV) != 1 {
			t.Fatalf("Apple TV tiles = %d, want 1", len(appleTV))
		}
		if appleTV[0].Offer != "rent" {
			t.Errorf("Apple TV offer = %q, want rent — rent beats buy", appleTV[0].Offer)
		}
	})

	t.Run("orders by offer, then TMDB's display priority, then name", func(t *testing.T) {
		t.Parallel()
		got := common.StreamingFrom(snapFor(t, videotest.MatrixDetailsBody), "US")
		if got == nil {
			t.Fatal("streaming = nil")
		}

		want := []string{"HBO Max", "Netflix", "Freevee", "Apple TV", "Amazon Video"}
		if len(got.Providers) != len(want) {
			t.Fatalf("providers = %+v, want %d entries", got.Providers, len(want))
		}
		for i, name := range want {
			if got.Providers[i].Name != name {
				t.Fatalf("provider %d = %q, want %q (order: %+v)", i, got.Providers[i].Name, name, got.Providers)
			}
		}
	})

	t.Run("logos are locally proxied, never a provider URL", func(t *testing.T) {
		t.Parallel()
		got := common.StreamingFrom(snapFor(t, videotest.MatrixDetailsBody), "US")
		if got == nil {
			t.Fatal("streaming = nil")
		}

		for _, p := range got.Providers {
			switch p.Name {
			case "HBO Max":
				// No logo_path in the fixture: null, so the SPA falls back to
				// its monogram rather than rendering a broken image.
				if p.Logo != nil {
					t.Errorf("HBO Max logo = %v, want null", p.Logo)
				}
			default:
				if p.Logo == nil {
					t.Fatalf("%s logo = null, want a proxy URL", p.Name)
				}
				if want := "api/v1/video/posters/" + common.PosterSizeThumb + "/"; len(*p.Logo) <= len(want) || (*p.Logo)[:len(want)] != want {
					t.Errorf("%s logo = %q, want a %s… proxy path", p.Name, *p.Logo, want)
				}
			}
		}
	})

	t.Run("the row is capped", func(t *testing.T) {
		t.Parallel()
		// One bucket, more services than the cap — the cap is presentation,
		// so the snapshot keeps them all and only the row is trimmed.
		body := `{"watch/providers": {"results": {"US": {"flatrate": [`
		for i := range common.StreamingMax + 5 {
			if i > 0 {
				body += ","
			}
			body += fmt.Sprintf(`{"provider_id": %d, "provider_name": "Service %02d", "display_priority": %d}`, i+1, i, i)
		}
		body += `]}}}}`
		if got := common.StreamingFrom(snapFor(t, body), "US"); got == nil || len(got.Providers) != common.StreamingMax {
			t.Errorf("providers = %d, want the cap of %d", len(got.Providers), common.StreamingMax)
		}
	})
}

// showWithProvidersBody is ShowWithOneSeasonBody plus the appended watch
// providers — what a /tv/{id} body looks like once "where to watch" rides the
// call (D-022). The season sub-document deliberately carries none: TMDB
// publishes providers per show, never per season.
const showWithProvidersBody = `{
  "id": 1396, "name": "Breaking Bad",
  "first_air_date": "2008-01-20",
  "seasons": [
    {"season_number": 1, "name": "Season 1", "episode_count": 7, "air_date": "2008-01-20"}
  ],
  "watch/providers": {"results": {
    "US": {
      "link": "https://watch.example/tv/1396?locale=US",
      "flatrate": [{"provider_id": 8, "provider_name": "Netflix",
                    "logo_path": "/nf.jpg", "display_priority": 3}]
    }
  }},
  "recommendations": {"results": []},
  "season/1": {
    "_id": "52542272760ee31328000094", "season_number": 1, "name": "Season 1",
    "air_date": "2008-01-20", "vote_average": 8.1,
    "episodes": [
      {"episode_number": 1, "name": "Pilot", "air_date": "2008-01-20",
       "runtime": 58, "still_path": "/p1.jpg", "vote_average": 8.2, "vote_count": 1500}
    ]
  }
}`

func TestStreamingOnDetailViews(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("a show snapshot predating the section refreshes once, even when fresh", func(t *testing.T) {
		t.Parallel()
		// The shape every stored show had before watch/providers was appended.
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/tv/1396": `{"id": 1396, "name": "Breaking Bad", "seasons": []}`,
		}}
		svc, _ := detailsService(t, stub)

		if _, err := svc.MaterialiseShow(ctx, 1396); err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		if stub.CallCount("/3/tv/1396") != 1 {
			t.Fatalf("tmdb calls after materialising = %d, want 1", stub.CallCount("/3/tv/1396"))
		}

		// Same seasonless shape plus the appended section: a new season would
		// cost its own /tv/{id} call and blur the count this test is about.
		stub.Mu.Lock()
		stub.Bodies["/3/tv/1396"] = `{"id": 1396, "name": "Breaking Bad", "seasons": [],
			"aggregate_credits": {"cast": []}, "recommendations": {"results": []},
			"watch/providers": {"results": {"US": {"flatrate": [
				{"provider_id": 8, "provider_name": "Netflix", "logo_path": "/nf.jpg",
				 "display_priority": 3}]}}}}`
		stub.Mu.Unlock()

		d, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{TMDBID: "1396"})
		if err != nil {
			t.Fatalf("ShowDetails: %v", err)
		}
		if stub.CallCount("/3/tv/1396") != 2 {
			t.Errorf("tmdb calls after the view = %d, want 2 — the missing section forces one refresh",
				stub.CallCount("/3/tv/1396"))
		}
		if d.Streaming == nil || len(d.Streaming.Providers) != 1 ||
			d.Streaming.Providers[0].Name != "Netflix" {
			t.Fatalf("streaming = %+v, want Netflix after the refresh", d.Streaming)
		}

		// And exactly once: the refetched body carries the key, so a third
		// view answers from the snapshot.
		if _, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{TMDBID: "1396"}); err != nil {
			t.Fatalf("third ShowDetails: %v", err)
		}
		if stub.CallCount("/3/tv/1396") != 2 {
			t.Errorf("tmdb calls after third view = %d, want still 2", stub.CallCount("/3/tv/1396"))
		}
	})

	// The regression this whole slice tripped over: ShowDetails used to gate
	// staleness on the tmdb_id branch only, so a show reached by its LOCAL id —
	// how the watchlist links it, and how the show page re-reads itself after
	// any write — never refreshed. Not on age, not on a widened append, not on
	// a changed region.
	t.Run("a show reached by local id refreshes exactly like one reached by tmdb_id", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{
			"/3/tv/1396": `{"id": 1396, "name": "Breaking Bad", "seasons": []}`,
		}}
		svc, _ := detailsService(t, stub)

		row, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		stub.Mu.Lock()
		stub.Bodies["/3/tv/1396"] = `{"id": 1396, "name": "Breaking Bad", "seasons": [],
			"aggregate_credits": {"cast": []}, "recommendations": {"results": []},
			"watch/providers": {"results": {"US": {"flatrate": [
				{"provider_id": 8, "provider_name": "Netflix", "logo_path": "/nf.jpg",
				 "display_priority": 3}]}}}}`
		stub.Mu.Unlock()
		before := stub.CallCount("/3/tv/1396")

		d, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{ID: fmt.Sprint(row.ID)})
		if err != nil {
			t.Fatalf("ShowDetails by id: %v", err)
		}
		if stub.CallCount("/3/tv/1396") != before+1 {
			t.Fatalf("tmdb calls = %d, want one refresh — the id path skipped the gate",
				stub.CallCount("/3/tv/1396"))
		}
		if d.Streaming == nil || len(d.Streaming.Providers) != 1 {
			t.Fatalf("streaming = %+v, want the refreshed listing", d.Streaming)
		}
		// And exactly once, like the tmdb_id path.
		if _, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{ID: fmt.Sprint(row.ID)}); err != nil {
			t.Fatalf("second ShowDetails by id: %v", err)
		}
		if stub.CallCount("/3/tv/1396") != before+1 {
			t.Errorf("tmdb calls = %d, want still %d", stub.CallCount("/3/tv/1396"), before+1)
		}
	})

	// Older than the providers work: the same missing gate meant an id-addressed
	// show never honoured ShowStaleAfter either.
	t.Run("a show reached by local id also honours plain time staleness", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": showWithProvidersBody}}
		svc, clock := detailsService(t, stub)

		row, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		before := stub.CallCount("/3/tv/1396")

		*clock = clock.Add(common.ShowStaleAfter + time.Hour)
		if _, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{ID: fmt.Sprint(row.ID)}); err != nil {
			t.Fatalf("ShowDetails by id: %v", err)
		}
		if stub.CallCount("/3/tv/1396") <= before {
			t.Errorf("tmdb calls = %d, want a refresh past the %s window",
				stub.CallCount("/3/tv/1396"), common.ShowStaleAfter)
		}
	})

	t.Run("a season and its episode borrow the tracked show's listing", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": showWithProvidersBody}}
		svc, _ := detailsService(t, stub)

		if _, err := svc.MaterialiseShow(ctx, 1396); err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}

		season, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{TMDBID: "1396", Season: "1"})
		if err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		// The season's own snapshot is the season sub-document, which carries
		// no providers — so this can only have come from the show row.
		if season.Streaming == nil || len(season.Streaming.Providers) != 1 ||
			season.Streaming.Providers[0].Name != "Netflix" {
			t.Fatalf("season streaming = %+v, want the show's Netflix entry", season.Streaming)
		}
		if season.Streaming.Region != videotest.WatchRegion {
			t.Errorf("season region = %q, want %q", season.Streaming.Region, videotest.WatchRegion)
		}

		episode, err := svc.EpisodeDetails(ctx, 0, EpisodeDetailsParams{TMDBID: "1396", Season: "1", Episode: "1"})
		if err != nil {
			t.Fatalf("EpisodeDetails: %v", err)
		}
		// Both views project the same seasonRenderState call, so they cannot
		// disagree about what the show carries.
		if episode.Streaming == nil || len(episode.Streaming.Providers) != 1 ||
			episode.Streaming.Providers[0].Name != season.Streaming.Providers[0].Name {
			t.Errorf("episode streaming = %+v, want the season's %+v", episode.Streaming, season.Streaming)
		}
	})

	t.Run("an untracked show's season lists nothing, without a second TMDB call", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": showWithProvidersBody}}
		svc, _ := detailsService(t, stub)

		season, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{TMDBID: "1396", Season: "1"})
		if err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		if season.ID != nil {
			t.Fatalf("season id = %v, want null — viewing stores nothing (D-015)", season.ID)
		}
		// No show row to read, and reaching for TMDB here would be a provider
		// call on a render path. So the honest answer is "no answer" — null,
		// not an empty listing, which the page would render as "not available
		// in RO" off a lookup it never performed.
		if season.Streaming != nil {
			t.Errorf("streaming = %+v, want nil for an untracked show", season.Streaming)
		}
		if n := stub.CallCount("/3/tv/1396"); n != 1 {
			t.Errorf("tmdb calls = %d, want the one live season fetch", n)
		}
	})
}

func TestPruneWatchProviders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("keeps one country and marks which, leaving everything else alone", func(t *testing.T) {
		t.Parallel()
		pruned := common.PruneWatchProviders(json.RawMessage(videotest.MatrixDetailsBody), "US")

		if len(pruned) >= len(videotest.MatrixDetailsBody) {
			t.Errorf("pruned body is %d bytes, was %d — nothing was dropped", len(pruned), len(videotest.MatrixDetailsBody))
		}

		var body map[string]json.RawMessage
		if err := json.Unmarshal(pruned, &body); err != nil {
			t.Fatalf("pruned body does not parse: %v", err)
		}
		var kept common.PrunedWatchProviders
		if err := json.Unmarshal(body[common.WatchProvidersKey], &kept); err != nil {
			t.Fatalf("pruned section does not parse: %v", err)
		}
		if kept.SofalogRegion != "US" {
			t.Errorf("marker = %q, want US", kept.SofalogRegion)
		}
		if _, ok := kept.Results["DE"]; ok {
			t.Errorf("results still carry DE: %v", kept.Results)
		}
		if _, ok := kept.Results["US"]; !ok {
			t.Fatalf("results lost US: %v", kept.Results)
		}

		// Every other section is untouched, and the pruned body still projects
		// exactly like the full one did — the whole point of keeping `results`
		// keyed by region.
		snap := snapFor(t, string(pruned))
		if len(snap.Credits.Cast) != 2 || snap.VoteAverage != 8.2 {
			t.Errorf("pruning disturbed the rest of the snapshot: %+v", snap.Credits.Cast)
		}
		before := common.StreamingFrom(snapFor(t, videotest.MatrixDetailsBody), "US")
		after := common.StreamingFrom(snap, "US")
		if len(before.Providers) != len(after.Providers) || before.Providers[0] != after.Providers[0] {
			t.Errorf("projection changed: %+v then %+v", before.Providers, after.Providers)
		}
	})

	t.Run("a region the body does not list still gets its marker", func(t *testing.T) {
		t.Parallel()
		pruned := common.PruneWatchProviders(json.RawMessage(videotest.MatrixDetailsBody), "RO")
		// Without the marker an empty `results` would be indistinguishable
		// from "pruned to some other country", and a region change would never
		// refresh.
		if watchProvidersStale(string(pruned), "RO") {
			t.Errorf("a snapshot pruned to RO reads as stale for RO")
		}
		if !watchProvidersStale(string(pruned), "US") {
			t.Errorf("a snapshot pruned to RO must read as stale for US")
		}
		if got := common.StreamingFrom(snapFor(t, string(pruned)), "RO"); len(got.Providers) != 0 {
			t.Errorf("providers = %+v, want none", got.Providers)
		}
	})

	t.Run("a body it cannot use comes back untouched", func(t *testing.T) {
		t.Parallel()
		for _, body := range []string{`{not json`, `{"title": "No section"}`} {
			if got := common.PruneWatchProviders(json.RawMessage(body), "US"); string(got) != body {
				t.Errorf("pruned %q into %q, want it unchanged", body, got)
			}
		}
	})

	t.Run("changing the region refreshes each item once, on its next view", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/movie/603": videotest.MatrixDetailsBody}}
		svc, _ := detailsService(t, stub)

		first, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails: %v", err)
		}
		// Untracked, so nothing is stored yet — track it by refreshing, the
		// same write tracking performs.
		if _, err := svc.RefreshMovie(ctx, 603); err != nil {
			t.Fatalf("refreshMovie: %v", err)
		}
		calls := stub.CallCount("/3/movie/603")
		if _, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"}); err != nil {
			t.Fatalf("second MovieDetails: %v", err)
		}
		if stub.CallCount("/3/movie/603") != calls {
			t.Fatalf("a fresh snapshot in the configured region refetched: %d then %d",
				calls, stub.CallCount("/3/movie/603"))
		}
		if first.Streaming == nil || len(first.Streaming.Providers) == 0 {
			t.Fatalf("no US providers to begin with")
		}

		// The operator changes TMDB_WATCH_REGION. The stored body carries only
		// the old country, so it must refresh rather than render empty.
		svc.WatchRegion = "DE"
		d, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"})
		if err != nil {
			t.Fatalf("MovieDetails after the region change: %v", err)
		}
		if stub.CallCount("/3/movie/603") != calls+1 {
			t.Errorf("tmdb calls = %d, want one refresh after the region change",
				stub.CallCount("/3/movie/603"))
		}
		if d.Streaming == nil || len(d.Streaming.Providers) != 1 ||
			d.Streaming.Providers[0].Name != "WOW" {
			t.Fatalf("streaming = %+v, want the DE listing", d.Streaming)
		}
		if d.Streaming.Region != "DE" {
			t.Errorf("region = %q, want DE", d.Streaming.Region)
		}

		// And exactly once: the refetched body is pruned to DE and marked so.
		if _, err := svc.MovieDetails(ctx, 0, MovieDetailsParams{TMDBID: "603"}); err != nil {
			t.Fatalf("third MovieDetails: %v", err)
		}
		if stub.CallCount("/3/movie/603") != calls+1 {
			t.Errorf("tmdb calls = %d, want still %d", stub.CallCount("/3/movie/603"), calls+1)
		}
	})
}
