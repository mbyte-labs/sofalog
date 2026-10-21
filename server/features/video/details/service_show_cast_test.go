package details

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
)

// The show cast reads TMDB's aggregate_credits — everyone across every episode
// — and falls back to the latest-season credits a pre-switch snapshot holds.
// The season page reads its own season's aggregate credits through the same
// projection; its refresh gate is tested at the bottom of this file.
func TestShowCastEntries(t *testing.T) {
	t.Parallel()

	t.Run("aggregate credits: billing order, parts joined most-episodes-first, blanks dropped", func(t *testing.T) {
		t.Parallel()
		var snap common.MediaSnapshot
		if err := json.Unmarshal([]byte(`{"aggregate_credits": {"cast": [
			{"name": "Aaron Paul", "order": 1, "profile_path": "/ap.jpg", "total_episode_count": 62,
			 "roles": [{"character": "Jesse Pinkman", "episode_count": 62}]},
			{"name": "", "order": 0, "total_episode_count": 9,
			 "roles": [{"character": "Nobody", "episode_count": 9}]},
			{"name": "Extra", "order": 2, "total_episode_count": 1,
			 "roles": [{"character": "", "episode_count": 1}]},
			{"id": 17419, "name": "Bryan Cranston", "order": 0, "total_episode_count": 62,
			 "roles": [{"character": "Heisenberg", "episode_count": 3},
			           {"character": "Walter White", "episode_count": 62}]},
			{"name": "Anna Gunn", "order": 2, "total_episode_count": 60,
			 "roles": [{"character": "Skyler White", "episode_count": 60}]}
		]}}`), &snap); err != nil {
			t.Fatal(err)
		}

		got := common.ShowCastEntries(snap)
		names := make([]string, 0, len(got))
		for _, e := range got {
			names = append(names, e.Name)
		}
		// Ties on order break on episodes (Gunn before Extra), then TMDB's order.
		want := []string{"Bryan Cranston", "Aaron Paul", "Anna Gunn", "Extra"}
		if fmt.Sprint(names) != fmt.Sprint(want) {
			t.Fatalf("names = %v, want %v", names, want)
		}
		if got[0].Character != "Walter White / Heisenberg" {
			t.Errorf("character = %q, want the parts joined, most episodes first", got[0].Character)
		}
		if got[0].Episodes == nil || *got[0].Episodes != 62 {
			t.Errorf("episodes = %v, want 62", got[0].Episodes)
		}
		// A part TMDB left unnamed renders as no character, not as " / ".
		if got[3].Character != "" {
			t.Errorf("Extra's character = %q, want empty", got[3].Character)
		}
		if got[1].ProfilePath != "/ap.jpg" {
			t.Errorf("profile path = %q", got[1].ProfilePath)
		}
		// The person id rides through; an entry without one stays 0.
		if got[0].TMDBID != 17419 || got[1].TMDBID != 0 {
			t.Errorf("person ids = %d/%d, want 17419/0", got[0].TMDBID, got[1].TMDBID)
		}
	})

	t.Run("a snapshot with only the legacy credits section still yields its cast", func(t *testing.T) {
		t.Parallel()
		var snap common.MediaSnapshot
		if err := json.Unmarshal([]byte(`{"credits": {"cast": [
			{"id": 84497, "name": "Aaron Paul", "character": "Jesse", "order": 1, "profile_path": "/ap.jpg"},
			{"name": "", "character": "Nobody", "order": 0},
			{"name": "Bryan Cranston", "character": "Walter", "order": 0}
		]}}`), &snap); err != nil {
			t.Fatal(err)
		}
		got := common.ShowCastEntries(snap)
		if len(got) != 2 || got[0].Name != "Bryan Cranston" || got[1].Name != "Aaron Paul" {
			t.Fatalf("entries = %+v, want Cranston then Paul", got)
		}
		if got[1].TMDBID != 84497 || got[0].TMDBID != 0 {
			t.Errorf("person ids = %d/%d, want 0/84497", got[0].TMDBID, got[1].TMDBID)
		}
		if got[0].Character != "Walter" || got[0].Episodes != nil {
			t.Errorf("legacy entry = %+v, want its character and no episode count", got[0])
		}
	})

	t.Run("an aggregate section that lists nobody wins over a stale credits section", func(t *testing.T) {
		t.Parallel()
		// Present-but-empty is TMDB's answer, not a reason to fall back.
		var snap common.MediaSnapshot
		if err := json.Unmarshal([]byte(`{"aggregate_credits": {"cast": []},
			"credits": {"cast": [{"name": "Ghost", "order": 0}]}}`), &snap); err != nil {
			t.Fatal(err)
		}
		if got := common.ShowCastEntries(snap); len(got) != 0 {
			t.Errorf("entries = %+v, want none", got)
		}
	})

	t.Run("the asset set names the same photos the page does", func(t *testing.T) {
		t.Parallel()
		row := common.ShowRow{ID: 7, MetadataJSON: `{"aggregate_credits": {"cast": [
			{"name": "Aaron Paul", "order": 1, "profile_path": "/ap.jpg", "total_episode_count": 62,
			 "roles": [{"character": "Jesse", "episode_count": 62}]},
			{"name": "Bryan Cranston", "order": 0, "total_episode_count": 62,
			 "roles": [{"character": "Walter", "episode_count": 62}]}
		]}, "credits": {"cast": [{"name": "Old", "order": 0, "profile_path": "/old.jpg"}]}}`}
		desired := common.ComputeDesiredShowAssets(row, nil, nil)
		if _, ok := desired["assets/video/shows/7/cast/ap.jpg"]; !ok {
			t.Errorf("aggregate cast photo not desired: %v", keysOf(desired))
		}
		if _, ok := desired["assets/video/shows/7/cast/old.jpg"]; ok {
			t.Errorf("legacy credits photo desired beside an aggregate section: %v", keysOf(desired))
		}

		legacy := common.ShowRow{ID: 7, MetadataJSON: `{"credits": {"cast": [
			{"name": "Old", "order": 0, "profile_path": "/old.jpg"}]}}`}
		if _, ok := common.ComputeDesiredShowAssets(legacy, nil, nil)["assets/video/shows/7/cast/old.jpg"]; !ok {
			t.Errorf("a legacy snapshot's cast photo is no longer desired")
		}
	})
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A snapshot written while TVDetails appended `credits` has no aggregate
// section, and must refresh exactly once on its next view — by either address.
func TestShowCastRefreshGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Every stored show before the switch: providers pruned, no aggregate key.
	const legacyBody = `{"id": 1396, "name": "Breaking Bad", "seasons": [],
		"credits": {"cast": [{"name": "Bryan Cranston", "character": "Walter", "order": 0}]},
		"watch/providers": {"results": {}}}`
	const freshBody = `{"id": 1396, "name": "Breaking Bad", "seasons": [],
		"aggregate_credits": {"cast": [
			{"name": "Bryan Cranston", "order": 0, "total_episode_count": 62,
			 "roles": [{"character": "Walter White", "episode_count": 62}]},
			{"name": "Aaron Paul", "order": 1, "total_episode_count": 62,
			 "roles": [{"character": "Jesse Pinkman", "episode_count": 62}]}]},
		"watch/providers": {"results": {}},
		"recommendations": {"results": []}}`

	for _, byID := range []bool{false, true} {
		byID := byID
		name := "by tmdb_id"
		if byID {
			name = "by local id"
		}
		t.Run("a snapshot predating aggregate_credits refreshes once, even when fresh, "+name, func(t *testing.T) {
			t.Parallel()
			stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": legacyBody}}
			svc, _ := detailsService(t, stub)

			row, err := svc.MaterialiseShow(ctx, 1396)
			if err != nil {
				t.Fatalf("materialiseShow: %v", err)
			}
			stub.Mu.Lock()
			stub.Bodies["/3/tv/1396"] = freshBody
			stub.Mu.Unlock()
			before := stub.CallCount("/3/tv/1396")

			params := ShowDetailsParams{TMDBID: "1396"}
			if byID {
				params = ShowDetailsParams{ID: fmt.Sprint(row.ID)}
			}
			d, err := svc.ShowDetails(ctx, 0, params)
			if err != nil {
				t.Fatalf("ShowDetails: %v", err)
			}
			if stub.CallCount("/3/tv/1396") != before+1 {
				t.Fatalf("tmdb calls = %d, want %d — the missing section forces one refresh",
					stub.CallCount("/3/tv/1396"), before+1)
			}
			if len(d.Cast) != 2 || d.Cast[0].Episodes == nil || *d.Cast[0].Episodes != 62 {
				t.Fatalf("cast = %+v, want the aggregate cast after the refresh", d.Cast)
			}

			// And exactly once: the refetched body carries the key.
			if _, err := svc.ShowDetails(ctx, 0, params); err != nil {
				t.Fatalf("second ShowDetails: %v", err)
			}
			if stub.CallCount("/3/tv/1396") != before+1 {
				t.Errorf("tmdb calls = %d, want still %d", stub.CallCount("/3/tv/1396"), before+1)
			}
		})
	}

	t.Run("when the forced refresh fails the legacy cast is served", func(t *testing.T) {
		t.Parallel()
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": legacyBody}}
		svc, _ := detailsService(t, stub)
		if _, err := svc.MaterialiseShow(ctx, 1396); err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		stub.Mu.Lock()
		delete(stub.Bodies, "/3/tv/1396") // an unknown path is the stub's outage
		stub.Mu.Unlock()

		d, err := svc.ShowDetails(ctx, 0, ShowDetailsParams{TMDBID: "1396"})
		if err != nil {
			t.Fatalf("ShowDetails: %v — a tracked show never 503s", err)
		}
		if len(d.Cast) != 1 || d.Cast[0].Name != "Bryan Cranston" || d.Cast[0].Episodes != nil {
			t.Errorf("cast = %+v, want the legacy credits cast with no episode count", d.Cast)
		}
	})
}

// A season row written before the season's aggregate credits were appended has
// no `aggregate_credits` key at all; it must refresh exactly once on its next
// view, and keep rendering (with no cast) when that refresh fails.
func TestSeasonCastRefreshGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const body = `{"id": 1396, "name": "Breaking Bad", "seasons": [
			{"season_number": 1, "name": "Season 1", "episode_count": 1, "air_date": "2008-01-20"}],
		"season/1": {"season_number": 1, "name": "Season 1", "air_date": "2008-01-20",
			"episodes": [{"episode_number": 1, "name": "Pilot", "air_date": "2008-01-20"}]},
		"season/1/aggregate_credits": {"cast": [
			{"name": "Bryan Cranston", "order": 0, "total_episode_count": 1,
			 "roles": [{"character": "Walter White", "episode_count": 1}]}]},
		"season/1/credits": {"cast": [
			{"name": "Bryan Cranston", "character": "Walter White", "order": 0}]}}`

	// materialise the show, then rewrite its season row as an older shape:
	// fresh, but missing the given appended sections.
	setup := func(t *testing.T, metadata string) (*Service, *videotest.ProviderStub, common.SeasonRow) {
		stub := &videotest.ProviderStub{Bodies: map[string]string{"/3/tv/1396": body}}
		svc, _ := detailsService(t, stub)
		show, err := svc.MaterialiseShow(ctx, 1396)
		if err != nil {
			t.Fatalf("materialiseShow: %v", err)
		}
		row, err := common.GetSeasonByTMDBKey(ctx, svc.DB, 1396, 1)
		if err != nil {
			t.Fatalf("season row: %v", err)
		}
		row.MetadataJSON = metadata
		row.FetchedAt = svc.Now().UTC().Format(time.RFC3339)
		err = svc.DB.InTx(ctx, func(tx db.Tx) error {
			var txErr error
			row, txErr = common.UpsertSeasonByTMDBKey(ctx, tx, show.ID, row)
			return txErr
		})
		if err != nil {
			t.Fatalf("rewriting season row: %v", err)
		}
		return svc, stub, row
	}

	t.Run("a season snapshot predating aggregate_credits refreshes once, even when fresh", func(t *testing.T) {
		t.Parallel()
		svc, stub, row := setup(t, `{"season_number": 1, "name": "Season 1", "episodes": []}`)
		before := stub.CallCount("/3/tv/1396")

		d, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{ID: fmt.Sprint(row.ID)})
		if err != nil {
			t.Fatalf("SeasonDetails: %v", err)
		}
		if stub.CallCount("/3/tv/1396") != before+1 {
			t.Fatalf("tmdb calls = %d, want %d — the missing section forces one refresh",
				stub.CallCount("/3/tv/1396"), before+1)
		}
		if len(d.Cast) != 1 || d.Cast[0].Name != "Bryan Cranston" || d.Cast[0].Episodes == nil || *d.Cast[0].Episodes != 1 {
			t.Fatalf("cast = %+v, want the season's aggregate cast after the refresh", d.Cast)
		}
		// And exactly once: the refetched document carries the key.
		if _, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{ID: fmt.Sprint(row.ID)}); err != nil {
			t.Fatalf("second SeasonDetails: %v", err)
		}
		if stub.CallCount("/3/tv/1396") != before+1 {
			t.Errorf("tmdb calls = %d, want still %d", stub.CallCount("/3/tv/1396"), before+1)
		}
	})

	// The regular credits were appended after the aggregate ones: a row with the
	// first but not the second is the shape every season stored in between has.
	t.Run("a season snapshot with aggregate but no regular credits refreshes once, and the episode page gets its cast", func(t *testing.T) {
		t.Parallel()
		svc, stub, row := setup(t, `{"season_number": 1, "name": "Season 1", "episodes": [],
			"aggregate_credits": {"cast": []}}`)
		before := stub.CallCount("/3/tv/1396")

		d, err := svc.EpisodeDetails(ctx, 0, EpisodeDetailsParams{SeasonID: fmt.Sprint(row.ID), Episode: "1"})
		if err != nil {
			t.Fatalf("EpisodeDetails: %v", err)
		}
		if stub.CallCount("/3/tv/1396") != before+1 {
			t.Fatalf("tmdb calls = %d, want %d — the missing credits section forces one refresh",
				stub.CallCount("/3/tv/1396"), before+1)
		}
		if len(d.Cast) != 1 || d.Cast[0].Name != "Bryan Cranston" || d.Cast[0].Episodes != nil {
			t.Fatalf("cast = %+v, want the season's regulars with no episode count", d.Cast)
		}
		if d.GuestStars == nil || len(d.GuestStars) != 0 {
			t.Errorf("guest_stars = %v, want an empty list — the fixture episode has none", d.GuestStars)
		}
		if _, err := svc.EpisodeDetails(ctx, 0, EpisodeDetailsParams{SeasonID: fmt.Sprint(row.ID), Episode: "1"}); err != nil {
			t.Fatalf("second EpisodeDetails: %v", err)
		}
		if stub.CallCount("/3/tv/1396") != before+1 {
			t.Errorf("tmdb calls = %d, want still %d", stub.CallCount("/3/tv/1396"), before+1)
		}
	})

	t.Run("when the forced refresh fails the snapshot is served with no cast", func(t *testing.T) {
		t.Parallel()
		svc, stub, row := setup(t, `{"season_number": 1, "name": "Season 1", "episodes": []}`)
		stub.Mu.Lock()
		delete(stub.Bodies, "/3/tv/1396")
		stub.Mu.Unlock()

		d, err := svc.SeasonDetails(ctx, 0, SeasonDetailsParams{ID: fmt.Sprint(row.ID)})
		if err != nil {
			t.Fatalf("SeasonDetails: %v — a season with a row never 503s", err)
		}
		if d.Cast == nil || len(d.Cast) != 0 {
			t.Errorf("cast = %v, want an empty list", d.Cast)
		}
	})
}
