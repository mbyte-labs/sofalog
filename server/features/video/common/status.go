package common

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// This file is the SINGLE WRITER of user_movie_states and user_show_states
// (D-010; /verify greps for violations). Status is a maintained
// denormalisation, reproducible from user_watch_history alone.
//
// A movie's only status is 'watched', so RecomputeMovieStatus has no user
// intent to preserve. A show has three, and one of them — 'dropped' — is user
// intent that a recompute must never destroy. RecomputeShowStatus carries it
// through structurally: every one of its four statements excludes 'dropped' in
// its own WHERE clause. There is deliberately no Go conditional doing that job,
// because a conditional can be forgotten by a fifth statement and a WHERE
// clause cannot.
//
// The two functions that DO write 'dropped' — SetShowDropped and
// ClearShowDropped, at the bottom of this file — belong here for the same
// reason: the single-writer rule is about the COLUMN, not about the recompute.
// They are the user's own two commands, and every statement above treats the
// value they write as immovable.

// RecomputeMovieStatus rewrites one (user, movie) status from the watch log.
// Idempotent; always called in the same transaction as the log change it
// follows. Movies are deliberately asymmetric with shows: "tracked but
// unwatched" is a watchlist row, not a status, so a state row with no watches
// left is deleted rather than kept.
func RecomputeMovieStatus(ctx context.Context, tx db.Tx, userID, movieID int64, now string) error {
	// Promote (and create the row if absent): a watch exists → 'watched'.
	// The SELECT's WHERE guard also disambiguates the ON CONFLICT parse.
	const promote = `
INSERT INTO user_movie_states (user_id, movie_id, status, updated_at)
SELECT ?, ?, 'watched', ?
WHERE EXISTS (SELECT 1 FROM user_watch_history wh
              WHERE wh.user_id = ? AND wh.target_type = 'movie' AND wh.target_id = ?)
ON CONFLICT (user_id, movie_id) DO UPDATE SET
    status     = 'watched',
    updated_at = excluded.updated_at`
	if _, err := tx.Exec(ctx, promote, userID, movieID, now, userID, movieID); err != nil {
		return err
	}

	// Collapse: a row with no watches left is not a state at all.
	const collapse = `
DELETE FROM user_movie_states
WHERE user_id = ? AND movie_id = ?
  AND NOT EXISTS (SELECT 1 FROM user_watch_history wh
                  WHERE wh.user_id = ? AND wh.target_type = 'movie' AND wh.target_id = ?)`
	_, err := tx.Exec(ctx, collapse, userID, movieID, userID, movieID)
	return err
}

// EligibleEpisode is D-012's completion predicate: aired, non-special. `e` must
// be the alias of the episodes table, and the single placeholder is today as
// YYYY-MM-DD (Go-computed — never a SQL date function, which would not be
// portable).
//
// This constant is an INVARIANT, not a convenience. RecomputeShowStatus, the
// bulk-mark insert in store_shows.go and the up-next only_released filter in
// store_upnext.go all splice this exact text: if any of their ideas of "every
// aired episode" differ by one row, a show reads 'watched' while still
// offering a next released episode — the worst state the UI can be in. Note
// where up-next splices it: its next-episode PICK deliberately ignores
// airedness (an unaired or undated next episode is served with aired=false),
// so the predicate gates only the only_released filter, never the pick.
//
// NEVER widen this to `air_date IS NULL OR …`. An episode TMDB has announced
// but not dated must not read as aired, or one such row makes a show
// permanently uncompletable.
const EligibleEpisode = `e.season_number > 0
        AND e.air_date IS NOT NULL AND e.air_date <= ?`

// RecomputeShowStatus rewrites one (user, show) status from the watch log plus
// the show's episodes. Idempotent; always called in the same transaction as the
// log change it follows, and exactly ONCE per operation — a bulk mark that
// wrote 200 watch rows calls this once, not 200 times.
//
// today is YYYY-MM-DD and now is RFC3339 UTC; both come from one s.now()
// reading so every statement in a request agrees on where "aired" ends.
//
// Shows are deliberately asymmetric with movies: a show keeps a state row while
// it has any watch at all, because 'watching' is a real, lasting state that
// 'watched' is not the only alternative to.
func RecomputeShowStatus(ctx context.Context, tx db.Tx, userID, showID int64, today, now string) error {
	// ENSURE: any episode watch at all means this show has a state. The row is
	// created as 'watching'; statements 2 and 3 own every transition after
	// that, which is why this is DO NOTHING and never DO UPDATE — DO UPDATE
	// would resurrect a user-set 'dropped' and would demote a 'watched' show
	// the moment a rewatch was logged.
	//
	// The WHERE EXISTS is load-bearing twice: it is the "reproducible from the
	// log alone" guard, and it disambiguates SQLite's INSERT … SELECT … ON
	// CONFLICT parse, exactly like RecomputeMovieStatus's promote above.
	//
	// The join through user_watch_history carries target_type = 'episode' in
	// the same WHERE (D-013 — episode 5 and movie 5 both exist), and sits
	// inside EXISTS so the log's fan-out cannot inflate anything.
	const ensure = `
INSERT INTO user_show_states (user_id, show_id, status, updated_at)
SELECT ?, ?, 'watching', ?
WHERE EXISTS (SELECT 1 FROM user_watch_history wh
              JOIN episodes e ON e.id = wh.target_id
              WHERE wh.user_id = ? AND wh.target_type = 'episode'
                AND e.show_id = ?)
ON CONFLICT (user_id, show_id) DO NOTHING`
	if _, err := tx.Exec(ctx, ensure, userID, showID, now, userID, showID); err != nil {
		return err
	}

	// DEMOTE: a completed show with something eligible left unwatched is in
	// progress again — which is what happens when a new episode airs.
	const demote = `
UPDATE user_show_states
SET status = 'watching', updated_at = ?
WHERE user_id = ? AND show_id = ?
  AND status = 'watched'
  AND EXISTS (
      SELECT 1 FROM episodes e
      WHERE e.show_id = user_show_states.show_id
        AND ` + EligibleEpisode + `
        AND NOT EXISTS (
            SELECT 1 FROM user_watch_history wh
            WHERE wh.user_id     = user_show_states.user_id
              AND wh.target_type = 'episode'
              AND wh.target_id   = e.id))`
	if _, err := tx.Exec(ctx, demote, now, userID, showID, today); err != nil {
		return err
	}

	// PROMOTE: nothing eligible left unwatched means completed.
	//
	// The first EXISTS is D-012's vacuous guard. Without it a show whose
	// episodes have all been announced but none aired would satisfy "no
	// eligible episode is unwatched" trivially and flash 'watched' the instant
	// it was tracked.
	const promote = `
UPDATE user_show_states
SET status = 'watched', updated_at = ?
WHERE user_id = ? AND show_id = ?
  AND status = 'watching'
  AND EXISTS (
      SELECT 1 FROM episodes e
      WHERE e.show_id = user_show_states.show_id
        AND ` + EligibleEpisode + `)
  AND NOT EXISTS (
      SELECT 1 FROM episodes e
      WHERE e.show_id = user_show_states.show_id
        AND ` + EligibleEpisode + `
        AND NOT EXISTS (
            SELECT 1 FROM user_watch_history wh
            WHERE wh.user_id     = user_show_states.user_id
              AND wh.target_type = 'episode'
              AND wh.target_id   = e.id))`
	if _, err := tx.Exec(ctx, promote, now, userID, showID, today, today); err != nil {
		return err
	}

	// COLLAPSE: a derived row with no watches left is not a state at all. A
	// 'dropped' row is not derived, so it survives — that exclusion is the
	// whole reason this is one statement rather than an unconditional delete.
	//
	// Reached by DeleteWatch removing a show's last episode watch. It was
	// written before that path existed, on the principle that this function is
	// a true function of the log — which is why adding the delete needed no
	// change here. status_test.go also drives it directly.
	const collapse = `
DELETE FROM user_show_states
WHERE user_id = ? AND show_id = ?
  AND status <> 'dropped'
  AND NOT EXISTS (SELECT 1 FROM user_watch_history wh
                  JOIN episodes e ON e.id = wh.target_id
                  WHERE wh.user_id = ? AND wh.target_type = 'episode'
                    AND e.show_id = ?)`
	_, err := tx.Exec(ctx, collapse, userID, showID, userID, showID)
	return err
}

// SetShowDropped records 'dropped', the ONE status that is user intent rather
// than a derivation (D-010). It lives in this file because the single-writer
// rule covers the column, not just the recompute.
//
// An UPDATE, never an upsert. A state row exists if and only if the log holds an
// episode watch for the show or the user already dropped it — that is exactly
// what RecomputeShowStatus's ensure and collapse above guarantee — so a missing
// row means the show was never started, and dropping must not fabricate a status
// out of nothing. The service refuses that case before reaching here; this
// statement simply cannot create a row.
//
// Reports whether the status actually moved, so an already-dropped show is an
// idempotent no-op rather than a fresh updated_at.
func SetShowDropped(ctx context.Context, tx db.Tx, userID, showID int64, now string) (bool, error) {
	const q = `
UPDATE user_show_states
SET status = 'dropped', updated_at = ?
WHERE user_id = ? AND show_id = ? AND status <> 'dropped'`
	res, err := tx.Exec(ctx, q, now, userID, showID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ClearShowDropped releases the drop, and 'watching' is a LANDING value rather
// than the answer: the caller runs RecomputeShowStatus in the same transaction,
// and only 'watching' is a status that function will act on. That is what makes
// a show dropped after finishing come back as 'watched' rather than 'watching'
// (SPEC §6), and what lets one whose watches have since been deleted collapse to
// no row at all.
//
// Idempotent, and reports whether anything moved: a show that is not dropped is
// left exactly as it is, which is also the caller's signal to skip the
// recompute.
func ClearShowDropped(ctx context.Context, tx db.Tx, userID, showID int64, now string) (bool, error) {
	const q = `
UPDATE user_show_states
SET status = 'watching', updated_at = ?
WHERE user_id = ? AND show_id = ? AND status = 'dropped'`
	res, err := tx.Exec(ctx, q, now, userID, showID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
