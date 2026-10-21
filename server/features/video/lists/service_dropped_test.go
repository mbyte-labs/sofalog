package lists

import (
	"context"
	"errors"
	"slices"
	"testing"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
	"mbyte.ro/sofalog/httpx"
)

// seedShowState writes one state row directly, so the dropped list can be
// read without the watch-and-drop path that would produce it.
func seedShowState(t *testing.T, database *db.DB, userID, showID int64, status, at string) {
	t.Helper()
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO user_show_states (user_id, show_id, status, updated_at)
			 VALUES (?, ?, ?, ?)`,
			userID, showID, status, at)
		return err
	})
	if err != nil {
		t.Fatalf("seeding show state: %v", err)
	}
}

func TestDropped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	type fixture struct {
		svc                        *Service
		alice                      int64
		alpha, bravo, charlie, del common.ShowRow
	}
	setup := func(t *testing.T) fixture {
		t.Helper()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		f := fixture{svc: svc, alice: videotest.SeedUser(t, svc.DB, "alice")}
		bob := videotest.SeedUser(t, svc.DB, "bob")
		f.alpha = seedShowSnapshot(t, svc.DB, 1, "alpha", videotest.Ptr("2001-01-01"), nil, `{}`)
		f.bravo = seedShowSnapshot(t, svc.DB, 2, "Bravo", nil, nil, `{}`)
		f.charlie = seedShowSnapshot(t, svc.DB, 3, "Charlie", videotest.Ptr("1999-01-01"), nil, `{}`)
		f.del = seedShowSnapshot(t, svc.DB, 4, "Delta", videotest.Ptr("2010-01-01"), nil, `{}`)
		watching := seedShowSnapshot(t, svc.DB, 5, "Watching", nil, nil, `{}`)
		finished := seedShowSnapshot(t, svc.DB, 6, "Finished", nil, nil, `{}`)

		seedShowState(t, svc.DB, f.alice, f.alpha.ID, "dropped", "2026-08-02T00:00:00Z")
		seedShowState(t, svc.DB, f.alice, f.bravo.ID, "dropped", "2026-08-03T00:00:00Z")
		seedShowState(t, svc.DB, f.alice, f.charlie.ID, "dropped", "2026-08-01T00:00:00Z")
		seedShowState(t, svc.DB, f.alice, watching.ID, "watching", "2026-08-09T00:00:00Z")
		seedShowState(t, svc.DB, f.alice, finished.ID, "watched", "2026-08-09T00:00:00Z")
		seedShowState(t, svc.DB, bob, f.del.ID, "dropped", "2026-08-09T00:00:00Z")
		return f
	}
	ids := func(entries []DroppedEntry) []int64 {
		var out []int64
		for _, e := range entries {
			if e.TargetType != "show" || e.Show == nil || e.Movie != nil {
				t.Fatalf("entry %+v is not a show", e)
			}
			out = append(out, e.Show.ID)
		}
		return out
	}

	t.Run("only this user's dropped shows, newest drop first, added_at is the drop", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		got, err := f.svc.Dropped(ctx, f.alice, DroppedParams{})
		if err != nil {
			t.Fatalf("Dropped: %v", err)
		}
		if want := []int64{f.bravo.ID, f.alpha.ID, f.charlie.ID}; !slices.Equal(ids(got), want) {
			t.Fatalf("ids = %v, want %v", ids(got), want)
		}
		if got[0].AddedAt != "2026-08-03T00:00:00Z" {
			t.Errorf("added_at = %q, want the state row's updated_at", got[0].AddedAt)
		}
		if got[0].Watched {
			t.Error("a dropped show reads watched")
		}
	})

	t.Run("orderings, undated last both ways", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		cases := []struct {
			p    DroppedParams
			want []int64
		}{
			{DroppedParams{Order: "asc"}, []int64{f.charlie.ID, f.alpha.ID, f.bravo.ID}},
			{DroppedParams{OrderBy: "title", Order: "asc"}, []int64{f.alpha.ID, f.bravo.ID, f.charlie.ID}},
			{DroppedParams{OrderBy: "title"}, []int64{f.charlie.ID, f.bravo.ID, f.alpha.ID}},
			{DroppedParams{OrderBy: "release_date", Order: "asc"}, []int64{f.charlie.ID, f.alpha.ID, f.bravo.ID}},
			{DroppedParams{OrderBy: "release_date"}, []int64{f.alpha.ID, f.charlie.ID, f.bravo.ID}},
			{DroppedParams{Limit: "1", Offset: "1"}, []int64{f.alpha.ID}},
		}
		for _, c := range cases {
			got, err := f.svc.Dropped(ctx, f.alice, c.p)
			if err != nil {
				t.Fatalf("Dropped(%+v): %v", c.p, err)
			}
			if !slices.Equal(ids(got), c.want) {
				t.Errorf("Dropped(%+v) = %v, want %v", c.p, ids(got), c.want)
			}
		}
	})

	t.Run("an empty list is a non-nil empty slice", func(t *testing.T) {
		t.Parallel()
		svc := &Service{Core: &common.Core{Now: videotest.FixedClock, DB: videotest.DB(t), WatchRegion: videotest.WatchRegion}}
		got, err := svc.Dropped(ctx, videotest.SeedUser(t, svc.DB, "alice"), DroppedParams{})
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("Dropped = %#v, %v; want [] and no error", got, err)
		}
	})

	t.Run("bad parameters are a validation error", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		for _, p := range []DroppedParams{{OrderBy: "rating"}, {Order: "up"}, {Limit: "0"}, {Offset: "-1"}} {
			if _, err := f.svc.Dropped(ctx, f.alice, p); !errors.Is(err, httpx.ErrValidation) {
				t.Errorf("Dropped(%+v) err = %v, want ErrValidation", p, err)
			}
		}
	})
}
