package common

import (
	"context"
	"database/sql"

	"mbyte.ro/sofalog/db"
)

// LibraryFlag is one catalog row as the person page's filmography needs it
// (D-027): the local id a card links by, and whether THIS user has watched it.
type LibraryFlag struct {
	ID int64
	// Watched is the watchlist page's meaning per kind — a movie: any watch
	// in the log; a show: COMPLETED. InHistory is the looser "have I seen any
	// of it": the same fact for a movie, but for a show any episode watch at
	// all, finished or not — what the person page's "Seen with" grid asks.
	Watched   bool
	InHistory bool
}

// LibraryMovieFlags returns every movie in the catalog keyed by tmdb_id, with
// this user's watched flag — any watch in the log, the watchlist page's movie
// meaning. Whole catalog rather than the person's credit ids: a variable-length
// IN list is a spliced fragment, and the catalog is a household's library —
// thousands of rows at most, each one indexed EXISTS probe — on a page that
// already waits on a live TMDB round trip. Revisit with a chunked IN if a
// catalog ever grows past what one scan answers in a few milliseconds.
//
// EXISTS, never a LEFT JOIN: user_watch_history fans out (a rewatch is a new
// row), so joining it would multiply the result.//
// The expression is selected twice: for a movie, watched and in-history are
// the same fact, and one scanner reads both queries' four columns.
func LibraryMovieFlags(ctx context.Context, d *db.DB, userID int64) (map[int64]LibraryFlag, error) {
	const q = `
SELECT m.id, m.tmdb_id,
       EXISTS (SELECT 1 FROM user_watch_history h
               WHERE h.user_id = ? AND h.target_type = 'movie' AND h.target_id = m.id),
       EXISTS (SELECT 1 FROM user_watch_history h
               WHERE h.user_id = ? AND h.target_type = 'movie' AND h.target_id = m.id)
FROM movies m`
	return scanLibraryFlags(d.Read(ctx, q, userID, userID))
}

// LibraryShowFlags is the show arm: watched means COMPLETED — the stored status
// reached 'watched' (D-012), the watchlist page's meaning of a ✓ on a show
// card. Never "started": a show with one episode watched is in the library
// (id set) but not watched.//
// InHistory reads the LOG, not the state row: any episode watch of the show,
// Specials included — "have I seen any of it" is a question about the log. The
// join carries target_type, because target_id alone identifies nothing (a
// movie watch on id 5 must not make episode 5's show look started).
func LibraryShowFlags(ctx context.Context, d *db.DB, userID int64) (map[int64]LibraryFlag, error) {
	const q = `
SELECT sh.id, sh.tmdb_id,
       EXISTS (SELECT 1 FROM user_show_states st
               WHERE st.user_id = ? AND st.show_id = sh.id AND st.status = 'watched'),
       EXISTS (SELECT 1 FROM user_watch_history h
               JOIN episodes e ON e.id = h.target_id
               WHERE h.user_id = ? AND h.target_type = 'episode' AND e.show_id = sh.id)
FROM shows sh`
	return scanLibraryFlags(d.Read(ctx, q, userID, userID))
}

// scanLibraryFlags folds (id, tmdb_id, watched, in_history) rows into a map by
// tmdb_id.
// Never nil: a person whose credits the library holds none of reads from an
// empty map like anyone else.
func scanLibraryFlags(rows *sql.Rows, err error) (map[int64]LibraryFlag, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]LibraryFlag{}
	for rows.Next() {
		var id, tmdbID int64
		var watched, inHistory bool
		if err := rows.Scan(&id, &tmdbID, &watched, &inHistory); err != nil {
			return nil, err
		}
		out[tmdbID] = LibraryFlag{ID: id, Watched: watched, InHistory: inHistory}
	}
	return out, rows.Err()
}
