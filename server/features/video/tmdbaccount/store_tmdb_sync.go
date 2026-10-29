package tmdbaccount

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

// SQL for the TMDB account link and the push outbox (D-029). Both tables are
// per-user state; every statement here is scoped by user_id.

// tmdbAccountRow is one user's linked TMDB account, session included. It never
// leaves the service layer: TMDBLinkStatus projects the public fields.
type tmdbAccountRow struct {
	UserID    int64
	SessionID string
	AccountID int64
	Username  string
	LinkedAt  string
	BrokenAt  *string
	// SetupDone and the three switches are the one-time setup's state
	// (D-036). The switches gate EnqueueTMDBSync per kind.
	SetupDone bool
	Sync      TMDBSyncKinds
}

// getTMDBAccount reads this user's link; sql.ErrNoRows becomes ErrNotFound.
func getTMDBAccount(ctx context.Context, d *db.DB, userID int64) (tmdbAccountRow, error) {
	const q = `
SELECT user_id, tmdb_session_id, tmdb_account_id, tmdb_username, linked_at, broken_at,
       setup_done, sync_ratings, sync_watchlist, sync_favorites
FROM user_tmdb_accounts
WHERE user_id = ?`
	var row tmdbAccountRow
	var setupDone, ratings, watchlist, favorites int
	err := d.ReadRow(ctx, q, userID).Scan(&row.UserID, &row.SessionID, &row.AccountID, &row.Username, &row.LinkedAt, &row.BrokenAt,
		&setupDone, &ratings, &watchlist, &favorites)
	if errors.Is(err, sql.ErrNoRows) {
		return tmdbAccountRow{}, fmt.Errorf("%w: no TMDB account is linked", httpx.ErrNotFound)
	}
	row.SetupDone = setupDone == 1
	row.Sync = TMDBSyncKinds{Ratings: ratings == 1, Watchlist: watchlist == 1, Favorites: favorites == 1}
	return row, err
}

// upsertTMDBAccount records a fresh link. Re-linking replaces the session and
// clears broken_at — that is how a broken link heals. The setup columns are
// deliberately absent from the DO UPDATE: re-linking keeps the setup and the
// switches, and resetTMDBSyncSetup is what the service calls when the account
// turned out to be a different one.
func upsertTMDBAccount(ctx context.Context, tx db.Tx, userID int64, sessionID string, accountID int64, username, at string) error {
	const q = `
INSERT INTO user_tmdb_accounts (user_id, tmdb_session_id, tmdb_account_id, tmdb_username, linked_at, broken_at)
VALUES (?, ?, ?, ?, ?, NULL)
ON CONFLICT (user_id) DO UPDATE SET
    tmdb_session_id = excluded.tmdb_session_id,
    tmdb_account_id = excluded.tmdb_account_id,
    tmdb_username   = excluded.tmdb_username,
    linked_at       = excluded.linked_at,
    broken_at       = NULL`
	_, err := tx.Exec(ctx, q, userID, sessionID, accountID, username, at)
	return err
}

func deleteTMDBAccount(ctx context.Context, tx db.Tx, userID int64) error {
	const q = `DELETE FROM user_tmdb_accounts WHERE user_id = ?`
	_, err := tx.Exec(ctx, q, userID)
	return err
}

// markTMDBAccountBroken records that TMDB refused the session. The first
// refusal's instant is kept: a later one changes nothing.
func markTMDBAccountBroken(ctx context.Context, tx db.Tx, userID int64, at string) error {
	const q = `
UPDATE user_tmdb_accounts SET broken_at = ?
WHERE user_id = ? AND broken_at IS NULL`
	_, err := tx.Exec(ctx, q, at, userID)
	return err
}

// resetTMDBSyncSetup forgets the setup and every switch: the user linked a
// DIFFERENT TMDB account, so the choices made for the old one no longer apply
// (D-036).
func resetTMDBSyncSetup(ctx context.Context, tx db.Tx, userID int64) error {
	const q = `
UPDATE user_tmdb_accounts
SET setup_done = 0, sync_ratings = 0, sync_watchlist = 0, sync_favorites = 0
WHERE user_id = ?`
	_, err := tx.Exec(ctx, q, userID)
	return err
}

// updateTMDBSyncKinds writes the three ongoing-sync switches (D-036). Bound as
// 0/1 explicitly: the columns are INTEGER on both engines.
func updateTMDBSyncKinds(ctx context.Context, tx db.Tx, userID int64, k TMDBSyncKinds) error {
	const q = `
UPDATE user_tmdb_accounts
SET sync_ratings = ?, sync_watchlist = ?, sync_favorites = ?
WHERE user_id = ?`
	_, err := tx.Exec(ctx, q, tmdbFlag(k.Ratings), tmdbFlag(k.Watchlist), tmdbFlag(k.Favorites), userID)
	return err
}

// setTMDBSetupDone marks the setup finished. Run at submit when nothing was
// pulled, otherwise at the end of the pull job (D-036).
func setTMDBSetupDone(ctx context.Context, tx db.Tx, userID int64) error {
	const q = `UPDATE user_tmdb_accounts SET setup_done = 1 WHERE user_id = ?`
	_, err := tx.Exec(ctx, q, userID)
	return err
}

func tmdbFlag(on bool) int {
	if on {
		return 1
	}
	return 0
}

// enqueueRatingBackfill queues every rating this user already holds — the
// setup's one-time Push (D-036). Runs in the setup's own transaction, after
// the link row was checked, so no link guard is needed — and deliberately no
// switch guard either: a Push with its switch off still sends once ("send
// what I have, then stop"). Seasons are skipped: TMDB has no season rating
// either (D-020). Returns the number of rows written.
func enqueueRatingBackfill(ctx context.Context, tx db.Tx, userID int64, now string) (int64, error) {
	const q = `
INSERT INTO user_tmdb_sync
    (user_id, kind, target_type, target_id, op, rating, attempts, next_retry_at, last_error, created_at, updated_at)
SELECT user_id, 'rating', target_type, target_id, 'set', rating, 0, NULL, NULL, ?, ?
FROM user_video_ratings
WHERE user_id = ? AND target_type IN ('movie', 'show', 'episode')
ON CONFLICT (user_id, kind, target_type, target_id) DO UPDATE SET
    op            = 'set',
    rating        = excluded.rating,
    attempts      = 0,
    next_retry_at = NULL,
    last_error    = NULL,
    updated_at    = excluded.updated_at`
	res, err := tx.Exec(ctx, q, now, now, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// enqueueWatchlistBackfill is enqueueRatingBackfill for the watchlist: every
// movie and show this user has listed, queued as an add. Same transaction and
// same guard reasoning as the rating backfill.
func enqueueWatchlistBackfill(ctx context.Context, tx db.Tx, userID int64, now string) (int64, error) {
	const q = `
INSERT INTO user_tmdb_sync
    (user_id, kind, target_type, target_id, op, rating, attempts, next_retry_at, last_error, created_at, updated_at)
SELECT user_id, 'watchlist', target_type, target_id, 'set', NULL, 0, NULL, NULL, ?, ?
FROM user_video_watchlist
WHERE user_id = ? AND target_type IN ('movie', 'show')
ON CONFLICT (user_id, kind, target_type, target_id) DO UPDATE SET
    op            = 'set',
    rating        = NULL,
    attempts      = 0,
    next_retry_at = NULL,
    last_error    = NULL,
    updated_at    = excluded.updated_at`
	res, err := tx.Exec(ctx, q, now, now, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// enqueueFavoriteBackfill is enqueueWatchlistBackfill for the favorites
// (D-030): every movie and show this user has favorited, queued as an add.
// Same transaction and same guard reasoning as the rating backfill.
func enqueueFavoriteBackfill(ctx context.Context, tx db.Tx, userID int64, now string) (int64, error) {
	const q = `
INSERT INTO user_tmdb_sync
    (user_id, kind, target_type, target_id, op, rating, attempts, next_retry_at, last_error, created_at, updated_at)
SELECT user_id, 'favorite', target_type, target_id, 'set', NULL, 0, NULL, NULL, ?, ?
FROM user_video_favorites
WHERE user_id = ? AND target_type IN ('movie', 'show')
ON CONFLICT (user_id, kind, target_type, target_id) DO UPDATE SET
    op            = 'set',
    rating        = NULL,
    attempts      = 0,
    next_retry_at = NULL,
    last_error    = NULL,
    updated_at    = excluded.updated_at`
	res, err := tx.Exec(ctx, q, now, now, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func deleteTMDBSyncForUser(ctx context.Context, tx db.Tx, userID int64) error {
	const q = `DELETE FROM user_tmdb_sync WHERE user_id = ?`
	_, err := tx.Exec(ctx, q, userID)
	return err
}

// deleteTMDBSyncKind drops this user's pending rows of one kind — a switch
// turned off takes its queue with it (D-036).
func deleteTMDBSyncKind(ctx context.Context, tx db.Tx, userID int64, kind string) error {
	const q = `DELETE FROM user_tmdb_sync WHERE user_id = ? AND kind = ?`
	_, err := tx.Exec(ctx, q, userID, kind)
	return err
}

func countTMDBSyncPending(ctx context.Context, d *db.DB, userID int64) (int, error) {
	const q = `SELECT COUNT(*) FROM user_tmdb_sync WHERE user_id = ?`
	var n int
	err := d.ReadRow(ctx, q, userID).Scan(&n)
	return n, err
}

// tmdbSyncRow is one due outbox row with the session (and, for the watchlist
// route, the account id) that will send it.
type tmdbSyncRow struct {
	ID         int64
	UserID     int64
	Kind       string
	TargetType string
	TargetID   int64
	Op         string
	Rating     *int
	Attempts   int
	UpdatedAt  string
	SessionID  string
	AccountID  int64
}

// dueTMDBSync lists the rows due at now, oldest first, capped at limit. The
// join supplies the session and skips broken links in one predicate.
func dueTMDBSync(ctx context.Context, d *db.DB, now string, limit int) ([]tmdbSyncRow, error) {
	const q = `
SELECT r.id, r.user_id, r.kind, r.target_type, r.target_id, r.op, r.rating, r.attempts, r.updated_at,
       a.tmdb_session_id, a.tmdb_account_id
FROM user_tmdb_sync r
JOIN user_tmdb_accounts a ON a.user_id = r.user_id AND a.broken_at IS NULL
WHERE r.next_retry_at IS NULL OR r.next_retry_at <= ?
ORDER BY r.id
LIMIT ?`
	rows, err := d.Read(ctx, q, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tmdbSyncRow
	for rows.Next() {
		var r tmdbSyncRow
		if err := rows.Scan(&r.ID, &r.UserID, &r.Kind, &r.TargetType, &r.TargetID, &r.Op, &r.Rating, &r.Attempts, &r.UpdatedAt, &r.SessionID, &r.AccountID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// settleTMDBSyncDone removes a sent (or definitively dropped) row. The
// updated_at guard is what makes an in-flight replace safe: a re-rating that
// landed while this row was being sent rewrote updated_at, so it stays and
// is sent on the next batch.
func settleTMDBSyncDone(ctx context.Context, tx db.Tx, id int64, updatedAt string) error {
	const q = `DELETE FROM user_tmdb_sync WHERE id = ? AND updated_at = ?`
	_, err := tx.Exec(ctx, q, id, updatedAt)
	return err
}

// deferTMDBSync records a failed attempt and when to try again. Same
// updated_at guard as settleTMDBSyncDone: a replaced row starts over.
func deferTMDBSync(ctx context.Context, tx db.Tx, id int64, updatedAt string, attempts int, retryAt, lastErr string) error {
	const q = `
UPDATE user_tmdb_sync
SET attempts = ?, next_retry_at = ?, last_error = ?
WHERE id = ? AND updated_at = ?`
	_, err := tx.Exec(ctx, q, attempts, retryAt, lastErr, id, updatedAt)
	return err
}

// syncTargetIDs is a local target's TMDB address: the movie's or SHOW's tmdb
// id, plus the position for an episode.
type syncTargetIDs struct {
	TMDBID  int64
	Season  int
	Episode int
}

// resolveTMDBSyncTarget maps a local (target_type, target_id) onto TMDB's
// address. Three compile-time queries picked by a switch — never a spliced
// table name. A missing catalog row is ErrNotFound: the row is stale and the
// worker drops it.
func resolveTMDBSyncTarget(ctx context.Context, d *db.DB, targetType string, targetID int64) (syncTargetIDs, error) {
	const (
		movieQ   = `SELECT tmdb_id FROM movies WHERE id = ?`
		showQ    = `SELECT tmdb_id FROM shows WHERE id = ?`
		episodeQ = `
SELECT s.tmdb_id, e.season_number, e.episode_number
FROM episodes e
JOIN shows s ON s.id = e.show_id
WHERE e.id = ?`
	)
	var out syncTargetIDs
	var err error
	switch targetType {
	case "movie":
		err = d.ReadRow(ctx, movieQ, targetID).Scan(&out.TMDBID)
	case "show":
		err = d.ReadRow(ctx, showQ, targetID).Scan(&out.TMDBID)
	case "episode":
		err = d.ReadRow(ctx, episodeQ, targetID).Scan(&out.TMDBID, &out.Season, &out.Episode)
	default:
		return syncTargetIDs{}, fmt.Errorf("%w: %q is not a rating sync target", httpx.ErrValidation, targetType)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return syncTargetIDs{}, fmt.Errorf("%w: %s %d no longer exists", httpx.ErrNotFound, targetType, targetID)
	}
	return out, err
}
