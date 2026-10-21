package lists

import (
	"context"
	"math"

	"mbyte.ro/sofalog/db"
)

// The dropped list is not a table of its own: it is a view over the state rows
// whose sticky user-set value is 'dropped' (D-010). Shows only — a movie
// cannot be dropped, its state CHECK admits 'watched' alone — and the row's
// updated_at is the instant it was dropped, because SetShowDropped stamps it
// and no recompute touches a dropped row.

// droppedFilter is one page of the dropped list: favoritesFilter without the
// kind filter, since every entry is a show.
type droppedFilter struct {
	UserID  int64
	OrderBy string // added | title | release_date
	Desc    bool
	Limit   int
	Offset  int
}

// listDroppedShows returns one ordered page of this user's dropped shows as
// watchlistTarget rows, so the service projects them through the list pages'
// own card helpers. AddedAt carries the drop instant. An INNER JOIN suffices:
// show_id is a real foreign key that cascades, so a state row cannot dangle.
// The ordering rules are listFavoriteTargets': compile-time ORDER BY
// constants, LOWER() for the engines' collations, `(x IS NULL)` for
// nulls-last, a show_id tie-break, and the LIMIT sentinel.
func listDroppedShows(ctx context.Context, d *db.DB, f droppedFilter) ([]watchlistTarget, error) {
	const base = `
SELECT st.show_id, st.updated_at
FROM user_show_states st
JOIN shows sh ON sh.id = st.show_id
WHERE st.user_id = ? AND st.status = 'dropped'`

	const (
		addedAsc = `
ORDER BY st.updated_at, st.show_id`
		addedDesc = `
ORDER BY st.updated_at DESC, st.show_id DESC`
		titleAsc = `
ORDER BY LOWER(sh.title), st.show_id`
		titleDesc = `
ORDER BY LOWER(sh.title) DESC, st.show_id DESC`
		releasedAsc = `
ORDER BY (sh.first_air_date IS NULL), sh.first_air_date, st.show_id`
		releasedDesc = `
ORDER BY (sh.first_air_date IS NULL), sh.first_air_date DESC, st.show_id DESC`
	)
	const page = `
LIMIT ? OFFSET ?`

	q := base
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

	rows, err := d.Read(ctx, q, f.UserID, limit, f.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []watchlistTarget
	for rows.Next() {
		t := watchlistTarget{TargetType: "show"}
		if err := rows.Scan(&t.TargetID, &t.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
