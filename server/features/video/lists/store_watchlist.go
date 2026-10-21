package lists

import (
	"context"
	"database/sql"
	"math"
	"strings"

	"mbyte.ro/sofalog/db"
)

// deleteWatchlist removes the target from the user's watchlist. Removing an
// absent row is a no-op, not an error — the operation is idempotent.
func deleteWatchlist(ctx context.Context, tx db.Tx, userID int64, targetType string, targetID int64) error {
	const q = `
DELETE FROM user_video_watchlist
WHERE user_id = ? AND target_type = ? AND target_id = ?`
	_, err := tx.Exec(ctx, q, userID, targetType, targetID)
	return err
}

// watchlistTarget is one row of the ordered, paged watchlist: which target it
// names and when it was listed. The catalog snapshot and this user's private
// state for it are read per target afterwards, by Service.Watchlist.
//
// The pair (TargetType, TargetID) is the identity — the id alone identifies
// nothing, because movie 5 and show 5 both exist (D-013).
type watchlistTarget struct {
	TargetType string // movie | show
	TargetID   int64
	AddedAt    string
}

// watchlistFilter is one page of the list. Limit <= 0 means unlimited, and
// Type "" or "all" means both kinds.
type watchlistFilter struct {
	UserID  int64
	OrderBy string // added | title | release_date
	Type    string // all | movie | show
	Desc    bool
	Limit   int
	Offset  int
	// MatchUsers narrows the list to targets EVERY listed user has watchlisted
	// too (D-028) — an intersection, one EXISTS per id. Empty means no
	// narrowing. The service has already validated each id against the opted-in
	// set; the query re-checks the consent flag anyway, so the invariant does
	// not depend on the caller.
	MatchUsers []int64
}

// matchUser is one account another user may intersect their watchlist with:
// the id the filter takes, and the label the picker shows. Deliberately no
// email — the label is derived here so the address never leaves the store.
type matchUser struct {
	ID    int64
	Label string
}

// listMatchUsers returns every OTHER account that has turned "Allow matching
// watchlist" on (D-028), ordered by label. Reading users from the video store
// is like reading movies: a shared table, SQL only — auth owns the writes.
func listMatchUsers(ctx context.Context, d *db.DB, userID int64) ([]matchUser, error) {
	const q = `
SELECT id, display_name, email
FROM users
WHERE match_watchlist = 1 AND id <> ?
ORDER BY LOWER(COALESCE(display_name, email)), id`
	rows, err := d.Read(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []matchUser
	for rows.Next() {
		var u matchUser
		var displayName sql.NullString
		var email string
		if err := rows.Scan(&u.ID, &displayName, &email); err != nil {
			return nil, err
		}
		u.Label = matchLabel(displayName.String, email)
		out = append(out, u)
	}
	return out, rows.Err()
}

// matchLabel is the D-019 fallback, applied server-side: the display name when
// set, else the email's local part — so the peer list can carry a label
// without carrying the address.
func matchLabel(displayName, email string) string {
	if displayName != "" {
		return displayName
	}
	if at := strings.IndexByte(email, '@'); at > 0 {
		return email[:at]
	}
	return email
}

// listWatchlistTargets returns one ordered page of this user's watchlist,
// movies and shows interleaved. Ordering and paging have a single definition,
// in SQL, shared by both arms — which is the whole reason this query joins both
// catalog tables instead of the service running one query per kind and merging:
// two pages cannot be merged without redoing LIMIT/OFFSET in Go.
//
// Each LEFT JOIN is guarded by the target_type its arm belongs to, so the two
// can never cross (D-013) and neither can fan out — both match on a primary key
// at most once. That is also why a LEFT JOIN is safe here where the history
// feed must use EXISTS: user_watch_history fans out, catalog tables do not.
//
// `(m.id IS NOT NULL OR sh.id IS NOT NULL)` drops a dangling target_id — a
// polymorphic column has no foreign key, so a deleted catalog row leaves one
// behind — BEFORE LIMIT, which is what makes a page exactly as long as the rows
// it returns.
//
// Only the three watchlist columns are selected. The snapshots are read per
// target afterwards, the way the history feed reads its episodes: two catalog
// shapes cannot share one row without scanning both column lists as nullable,
// and that cost lands on every page whether it holds a show or not. The price
// is one read per entry instead of one per page — pages are a screenful, and
// this is a local database.
func listWatchlistTargets(ctx context.Context, d *db.DB, f watchlistFilter) ([]watchlistTarget, error) {
	const base = `
SELECT wl.target_type, wl.target_id, wl.added_at
FROM user_video_watchlist wl
LEFT JOIN movies m ON wl.target_type = 'movie' AND m.id = wl.target_id
LEFT JOIN shows sh ON wl.target_type = 'show'  AND sh.id = wl.target_id
WHERE wl.user_id = ? AND (m.id IS NOT NULL OR sh.id IS NOT NULL)`
	const oneType = `
  AND wl.target_type = ?`
	// One copy per matched user, each with its own bound id — the same idiom
	// as oneType, only repeated: the fragment is a compile-time constant and
	// nothing but the repetition count varies, which is why this is not a
	// spliced IN list. The predicate carries target_type: the id alone
	// identifies nothing (D-013). The consent check rides inside the EXISTS so
	// an id whose owner opted out — or that names nobody — simply matches no
	// row; the privacy rule holds in SQL, not only in the service.
	const matchOne = `
  AND EXISTS (SELECT 1 FROM user_video_watchlist o
              JOIN users u ON u.id = o.user_id AND u.match_watchlist = 1
              WHERE o.user_id = ? AND o.target_type = wl.target_type AND o.target_id = wl.target_id)`

	// Every ORDER BY is a compile-time constant chosen by a switch — the
	// requested key never reaches the query as text. Each key COALESCEs the two
	// arms, because the same list holds both: a show's title is shows.title and
	// its release date is its FIRST AIR DATE. The guarded joins mean exactly one
	// arm is non-null per row, so a COALESCE is a choice, never a fallback.
	//
	// Each ends in (target_type, target_id), in the same direction, as a
	// deterministic tie-break: this table has no id column of its own, and that
	// pair is its PRIMARY KEY tail — and the only thing that separates movie 5
	// from show 5. It is not decorative: adding several items in one sitting
	// writes one added_at to many rows, and two items can share a date or a
	// title.
	//
	// added_at is fixed-width RFC3339 UTC, which compares chronologically as
	// plain text on both engines, so no date function is needed; ordering by it
	// matches idx_user_video_watchlist_recent.
	//
	// LOWER(...) because the engines disagree otherwise: SQLite's default TEXT
	// ordering is binary, so every capital sorts ahead of every lowercase, while
	// Postgres follows its collation. LOWER exists on both and makes them agree.
	//
	// Both date columns are nullable, and the engines disagree there too —
	// SQLite puts NULLs first ascending, Postgres last. `(x IS NULL)` sorts
	// 0 before 1 in SQLite and false before true in Postgres, so an undated item
	// lands at the end of an ascending page on both. NULLS LAST would be the
	// direct way to say it and is deliberately not used: SQLite only learned it
	// in 3.30.
	const (
		title = `LOWER(COALESCE(m.title, sh.title))`
		date  = `COALESCE(m.release_date, sh.first_air_date)`

		addedAsc = `
ORDER BY wl.added_at, wl.target_type, wl.target_id`
		addedDesc = `
ORDER BY wl.added_at DESC, wl.target_type DESC, wl.target_id DESC`
		titleAsc = `
ORDER BY ` + title + `, wl.target_type, wl.target_id`
		titleDesc = `
ORDER BY ` + title + ` DESC, wl.target_type DESC, wl.target_id DESC`
		releasedAsc = `
ORDER BY (` + date + ` IS NULL), ` + date + `, wl.target_type, wl.target_id`
		releasedDesc = `
ORDER BY (` + date + ` IS NULL), ` + date + ` DESC, wl.target_type DESC, wl.target_id DESC`
	)
	// Always emit both keywords and pass a sentinel count for the unlimited
	// case: SQLite requires LIMIT before OFFSET and rejects a bare OFFSET, while
	// Postgres rejects a negative LIMIT, so neither `LIMIT -1 OFFSET n` nor
	// `OFFSET n` alone runs on both. Both engines take a 64-bit LIMIT.
	const page = `
LIMIT ? OFFSET ?`

	q := base
	args := []any{f.UserID}
	if f.Type == "movie" || f.Type == "show" {
		q += oneType
		args = append(args, f.Type)
	}
	for _, id := range f.MatchUsers {
		q += matchOne
		args = append(args, id)
	}

	switch {
	case f.OrderBy == "title" && f.Desc:
		q += titleDesc
	case f.OrderBy == "title":
		q += titleAsc
	case f.OrderBy == "release_date" && f.Desc:
		q += releasedDesc
	case f.OrderBy == "release_date":
		q += releasedAsc
	case f.Desc:
		q += addedDesc
	default:
		q += addedAsc
	}

	limit := int64(f.Limit)
	if f.Limit <= 0 {
		limit = math.MaxInt64
	}
	q += page
	args = append(args, limit, f.Offset)

	rows, err := d.Read(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []watchlistTarget
	for rows.Next() {
		var t watchlistTarget
		if err := rows.Scan(&t.TargetType, &t.TargetID, &t.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
