package common_test

import (
	"context"
	"testing"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
)

func TestRecomputeShowStatus(t *testing.T) {
	t.Parallel()
	aired := videotest.Ptr("2026-01-01")
	unaired := videotest.Ptr("2027-01-01")

	t.Run("no watches at all leaves no row", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Breaking Bad")
		videotest.SeedEpisode(t, database, show, 1, 1, aired)

		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "" {
			t.Errorf("status = %q, want no row", got)
		}
	})

	t.Run("one episode watch starts watching", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Breaking Bad")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedEpisode(t, database, show, 1, 2, aired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)

		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watching" {
			t.Errorf("status = %q, want watching", got)
		}
	})

	t.Run("every aired non-special watched promotes to watched", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Breaking Bad")
		for n := 1; n <= 3; n++ {
			videotest.SeedWatch(t, database, user, "episode", videotest.SeedEpisode(t, database, show, 1, n, aired), videotest.StatusNow)
		}
		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watched" {
			t.Errorf("status = %q, want watched", got)
		}
	})

	// D-012's vacuous guard. Without the first EXISTS in the promote statement,
	// "no eligible episode is unwatched" is trivially true here and a freshly
	// tracked show flashes 'watched'.
	t.Run("a show with nothing aired yet stays watching", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Upcoming")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, unaired)
		videotest.SeedEpisode(t, database, show, 1, 2, unaired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)

		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watching" {
			t.Errorf("status = %q, want watching", got)
		}
	})

	t.Run("specials never gate completion", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Breaking Bad")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedEpisode(t, database, show, 0, 1, aired) // an unwatched special
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)

		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watched" {
			t.Errorf("status = %q, want watched — an unwatched special must not block completion", got)
		}
	})

	t.Run("a specials-only show never completes", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Specials only")
		e0 := videotest.SeedEpisode(t, database, show, 0, 1, aired)
		videotest.SeedWatch(t, database, user, "episode", e0, videotest.StatusNow)

		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watching" {
			t.Errorf("status = %q, want watching — zero eligible episodes cannot be complete", got)
		}
	})

	// The predicate must be `air_date IS NOT NULL AND air_date <= today`, never
	// `air_date IS NULL OR …`: one undated episode would otherwise make the
	// show permanently uncompletable.
	t.Run("an undated episode neither airs nor blocks", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Breaking Bad")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedEpisode(t, database, show, 1, 2, nil) // announced, undated
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)

		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watched" {
			t.Errorf("status = %q, want watched", got)
		}
	})

	t.Run("a newly aired episode demotes a watched show", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Returning")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)
		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watched" {
			t.Fatalf("precondition: status = %q, want watched", got)
		}

		videotest.SeedEpisode(t, database, show, 2, 1, aired) // the new episode
		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watching" {
			t.Errorf("status = %q, want watching", got)
		}
	})

	t.Run("a rewatch never demotes a watched show", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Breaking Bad")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)
		videotest.Recompute(t, database, user, show)

		videotest.SeedWatch(t, database, user, "episode", e1, "2026-08-19T12:00:00Z")
		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watched" {
			t.Errorf("status = %q, want watched", got)
		}
	})

	// 'dropped' is user intent. Every statement excludes it in SQL, so no path
	// through this function may write over it or delete it.
	t.Run("dropped is sticky through every path", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Abandoned")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		e2 := videotest.SeedEpisode(t, database, show, 1, 2, aired)
		videotest.SeedShowStatus(t, database, user, show, "dropped")

		// No watches: the collapse path.
		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "dropped" {
			t.Fatalf("after collapse: status = %q, want dropped", got)
		}
		// Some watches: the ensure and demote paths.
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)
		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "dropped" {
			t.Fatalf("after ensure: status = %q, want dropped", got)
		}
		// All watches: the promote path.
		videotest.SeedWatch(t, database, user, "episode", e2, videotest.StatusNow)
		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "dropped" {
			t.Errorf("after promote: status = %q, want dropped", got)
		}

		var updated string
		if err := database.ReadRow(context.Background(),
			`SELECT updated_at FROM user_show_states WHERE user_id = ? AND show_id = ?`,
			user, show).Scan(&updated); err != nil {
			t.Fatalf("reading updated_at: %v", err)
		}
		if updated != "2026-08-01T00:00:00Z" {
			t.Errorf("updated_at = %q, want it untouched", updated)
		}
	})

	// Unreachable today — nothing deletes a watch — so it is driven directly.
	// It is what makes the recompute a true function of the log.
	t.Run("collapse removes a derived row with no watches left", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Breaking Bad")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)
		videotest.Recompute(t, database, user, show)

		err := database.InTx(context.Background(), func(tx db.Tx) error {
			_, err := tx.Exec(context.Background(),
				`DELETE FROM user_watch_history WHERE user_id = ?`, user)
			return err
		})
		if err != nil {
			t.Fatalf("deleting watches: %v", err)
		}
		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "" {
			t.Errorf("status = %q, want no row", got)
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Breaking Bad")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedEpisode(t, database, show, 1, 2, aired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)

		videotest.Recompute(t, database, user, show)
		videotest.Recompute(t, database, user, show)
		if n := videotest.CountRows(t, database, `SELECT COUNT(*) FROM user_show_states`); n != 1 {
			t.Errorf("state rows = %d, want 1", n)
		}
		if got := videotest.ShowStatus(t, database, user, show); got != "watching" {
			t.Errorf("status = %q, want watching", got)
		}
	})

	// D-013: ids are unique only within their own table, so every polymorphic
	// predicate must carry its target_type. A movie watch on id N must not make
	// episode N look watched.
	t.Run("a movie watch never counts as an episode watch", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Breaking Bad")
		episode := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		// A movie whose id collides with the episode's.
		movie := videotest.SeedMovie(t, database, 603, "The Matrix", nil, nil, `{}`)
		if movie.ID != episode {
			t.Skipf("ids did not collide (movie %d, episode %d)", movie.ID, episode)
		}
		videotest.SeedWatch(t, database, user, "movie", movie.ID, videotest.StatusNow)

		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "" {
			t.Errorf("status = %q, want no row — a movie watch is not an episode watch", got)
		}
	})
}

// dropAt / undropAt drive the two user-set writers the way `recompute` drives
// the derived ones, so a failure below points at their SQL rather than at a
// service.
func dropAt(t *testing.T, database *db.DB, userID, showID int64, now string) bool {
	t.Helper()
	var changed bool
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		var err error
		changed, err = common.SetShowDropped(context.Background(), tx, userID, showID, now)
		return err
	})
	if err != nil {
		t.Fatalf("setShowDropped: %v", err)
	}
	return changed
}

func undropAt(t *testing.T, database *db.DB, userID, showID int64, now string) bool {
	t.Helper()
	var changed bool
	err := database.InTx(context.Background(), func(tx db.Tx) error {
		var err error
		changed, err = common.ClearShowDropped(context.Background(), tx, userID, showID, now)
		if err != nil || !changed {
			return err
		}
		return common.RecomputeShowStatus(context.Background(), tx, userID, showID, videotest.StatusToday, now)
	})
	if err != nil {
		t.Fatalf("clearShowDropped: %v", err)
	}
	return changed
}

// The other half of D-010: 'dropped' is the one value a user writes directly,
// and these two statements are the only ones that may. The tests above prove
// nothing else can produce or destroy it; these prove the pair that can.
func TestDropAndUndropShow(t *testing.T) {
	t.Parallel()
	aired := videotest.Ptr("2026-01-01")
	unaired := videotest.Ptr("2027-01-01")

	t.Run("drops a show in progress", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Abandoned")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedEpisode(t, database, show, 1, 2, aired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)
		videotest.Recompute(t, database, user, show)

		if !dropAt(t, database, user, show, videotest.StatusNow) {
			t.Error("changed = false, want true")
		}
		if got := videotest.ShowStatus(t, database, user, show); got != "dropped" {
			t.Errorf("status = %q, want dropped", got)
		}
	})

	t.Run("drops a finished show", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Done")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)
		videotest.Recompute(t, database, user, show)
		if got := videotest.ShowStatus(t, database, user, show); got != "watched" {
			t.Fatalf("status = %q, want watched before the drop", got)
		}

		dropAt(t, database, user, show, videotest.StatusNow)
		if got := videotest.ShowStatus(t, database, user, show); got != "dropped" {
			t.Errorf("status = %q, want dropped", got)
		}
	})

	// UPDATE, never an upsert: a row exists iff the log holds an episode watch
	// or the user already dropped the show, so dropping must not be able to
	// invent a status for a show that was never started.
	t.Run("writes nothing when there is no state row", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Unstarted")
		videotest.SeedEpisode(t, database, show, 1, 1, aired)

		if dropAt(t, database, user, show, videotest.StatusNow) {
			t.Error("changed = true, want false")
		}
		if got := videotest.ShowStatus(t, database, user, show); got != "" {
			t.Errorf("status = %q, want no row", got)
		}
	})

	t.Run("a second drop changes nothing at all", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Abandoned")
		videotest.SeedShowStatus(t, database, user, show, "dropped")

		if dropAt(t, database, user, show, videotest.StatusNow) {
			t.Error("changed = true, want false")
		}
		var updated string
		if err := database.ReadRow(context.Background(),
			`SELECT updated_at FROM user_show_states WHERE user_id = ? AND show_id = ?`,
			user, show).Scan(&updated); err != nil {
			t.Fatalf("reading updated_at: %v", err)
		}
		if updated != "2026-08-01T00:00:00Z" {
			t.Errorf("updated_at = %q, want it untouched", updated)
		}
	})

	// SPEC §6: un-dropping recomputes from the log, so a show dropped after
	// finishing returns to 'watched' — NOT to the 'watching' the clear itself
	// lands on.
	t.Run("un-dropping a finished show returns it to watched", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Done")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)
		videotest.SeedShowStatus(t, database, user, show, "dropped")

		if !undropAt(t, database, user, show, videotest.StatusNow) {
			t.Error("changed = false, want true")
		}
		if got := videotest.ShowStatus(t, database, user, show); got != "watched" {
			t.Errorf("status = %q, want watched", got)
		}
	})

	t.Run("un-dropping a partly watched show returns it to watching", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Halfway")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedEpisode(t, database, show, 1, 2, aired)
		videotest.SeedEpisode(t, database, show, 1, 3, unaired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)
		videotest.SeedShowStatus(t, database, user, show, "dropped")

		undropAt(t, database, user, show, videotest.StatusNow)
		if got := videotest.ShowStatus(t, database, user, show); got != "watching" {
			t.Errorf("status = %q, want watching", got)
		}
	})

	// The residual case: a drop survives its own watches being deleted (the
	// collapse skips 'dropped'), so releasing it must leave no row rather than
	// a 'watching' state with nothing behind it.
	t.Run("un-dropping a show with no watches collapses the row", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Abandoned")
		videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedShowStatus(t, database, user, show, "dropped")

		undropAt(t, database, user, show, videotest.StatusNow)
		if got := videotest.ShowStatus(t, database, user, show); got != "" {
			t.Errorf("status = %q, want no row", got)
		}
	})

	t.Run("un-dropping a show that is not dropped is a no-op", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		user := videotest.SeedUser(t, database, "a@example.com")
		show := videotest.SeedShow(t, database, 1396, "Watching")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedEpisode(t, database, show, 1, 2, aired)
		videotest.SeedWatch(t, database, user, "episode", e1, videotest.StatusNow)
		videotest.Recompute(t, database, user, show)

		if undropAt(t, database, user, show, videotest.StatusNow) {
			t.Error("changed = true, want false")
		}
		if got := videotest.ShowStatus(t, database, user, show); got != "watching" {
			t.Errorf("status = %q, want watching", got)
		}
	})

	// One user's drop is their own. The pair (user_id, show_id) is the key, and
	// both statements carry both halves.
	t.Run("never touches another user's row", func(t *testing.T) {
		t.Parallel()
		database := videotest.DB(t)
		mine := videotest.SeedUser(t, database, "a@example.com")
		theirs := videotest.SeedUser(t, database, "b@example.com")
		show := videotest.SeedShow(t, database, 1396, "Shared")
		e1 := videotest.SeedEpisode(t, database, show, 1, 1, aired)
		videotest.SeedWatch(t, database, mine, "episode", e1, videotest.StatusNow)
		videotest.SeedWatch(t, database, theirs, "episode", e1, videotest.StatusNow)
		videotest.Recompute(t, database, mine, show)
		videotest.Recompute(t, database, theirs, show)

		dropAt(t, database, mine, show, videotest.StatusNow)
		if got := videotest.ShowStatus(t, database, theirs, show); got != "watched" {
			t.Errorf("their status = %q, want watched", got)
		}
	})
}
