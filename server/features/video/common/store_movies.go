package common

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

// MovieRow mirrors the movies catalog table. Nullable columns are pointers.
type MovieRow struct {
	ID            int64
	TMDBID        int64
	Title         string
	OriginalTitle *string
	ReleaseDate   *string
	RuntimeMin    *int
	Overview      *string
	Tagline       *string
	GenresJSON    *string
	PosterPath    *string
	BackdropPath  *string
	MetadataJSON  string
	FetchedAt     string
}

const movieColumns = `id, tmdb_id, title, original_title, release_date, runtime_min,
overview, tagline, genres, poster_path, backdrop_path, metadata_json, fetched_at`

// rowScanner is what *sql.Row and *sql.Rows have in common, so the movieColumns
// field list has exactly one Scan call behind it whichever a query returns.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanMovieInto reads movieColumns, in order, off s. A list query selecting
// extra trailing columns passes them after these — Scan takes the destinations
// positionally, so the caller appends its own.
func scanMovieInto(s rowScanner, m *MovieRow, extra ...any) error {
	dest := append([]any{&m.ID, &m.TMDBID, &m.Title, &m.OriginalTitle, &m.ReleaseDate,
		&m.RuntimeMin, &m.Overview, &m.Tagline, &m.GenresJSON, &m.PosterPath,
		&m.BackdropPath, &m.MetadataJSON, &m.FetchedAt}, extra...)
	return s.Scan(dest...)
}

func scanMovie(row *sql.Row) (MovieRow, error) {
	var m MovieRow
	err := scanMovieInto(row, &m)
	if errors.Is(err, sql.ErrNoRows) {
		return MovieRow{}, fmt.Errorf("%w: no such movie", httpx.ErrNotFound)
	}
	if err != nil {
		return MovieRow{}, err
	}
	return m, nil
}

func GetMovieByID(ctx context.Context, d *db.DB, id int64) (MovieRow, error) {
	const q = `SELECT ` + movieColumns + ` FROM movies WHERE id = ?`
	return scanMovie(d.ReadRow(ctx, q, id))
}

func GetMovieByTMDBID(ctx context.Context, d *db.DB, tmdbID int64) (MovieRow, error) {
	const q = `SELECT ` + movieColumns + ` FROM movies WHERE tmdb_id = ?`
	return scanMovie(d.ReadRow(ctx, q, tmdbID))
}

// MovieReferenced reports whether ANY user references the movie — the D-008
// reference set: a state row, a watchlist row, a favorite row (D-030), any
// watch, a rating, or an owned copy (D-021). Every polymorphic predicate
// carries target_type='movie', because movie N and episode N both exist
// (D-013).
func MovieReferenced(ctx context.Context, d *db.DB, movieID int64) (bool, error) {
	const q = `
SELECT EXISTS (SELECT 1 FROM user_movie_states    WHERE movie_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_watchlist WHERE target_type = 'movie' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_favorites WHERE target_type = 'movie' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_watch_history   WHERE target_type = 'movie' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_ratings   WHERE target_type = 'movie' AND target_id = ?)
    OR EXISTS (SELECT 1 FROM user_video_copies    WHERE target_type = 'movie' AND target_id = ?)`
	var referenced bool
	err := d.ReadRow(ctx, q, movieID, movieID, movieID, movieID, movieID, movieID).Scan(&referenced)
	return referenced, err
}

// userMovieState is one user's whole private state for one movie — the
// truthful initial state of every control on the details page.
type userMovieState struct {
	Watched     bool
	Watchlisted bool
	Favorited   bool
	Rating      *int // this user's rating, 1-10; nil when unrated
}

// UserMovieFlags returns this user's state for the movie in one round trip.
// The rating is a scalar subquery rather than a join: user_watch_history fans
// out (a rewatch is a new row), so joining it would multiply the result.
func UserMovieFlags(ctx context.Context, d *db.DB, userID, movieID int64) (userMovieState, error) {
	const q = `
SELECT EXISTS (SELECT 1 FROM user_watch_history
               WHERE user_id = ? AND target_type = 'movie' AND target_id = ?),
       EXISTS (SELECT 1 FROM user_video_watchlist
               WHERE user_id = ? AND target_type = 'movie' AND target_id = ?),
       EXISTS (SELECT 1 FROM user_video_favorites
               WHERE user_id = ? AND target_type = 'movie' AND target_id = ?),
       (SELECT rating FROM user_video_ratings
        WHERE user_id = ? AND target_type = 'movie' AND target_id = ?)`
	var st userMovieState
	err := d.ReadRow(ctx, q, userID, movieID, userID, movieID, userID, movieID, userID, movieID).
		Scan(&st.Watched, &st.Watchlisted, &st.Favorited, &st.Rating)
	return st, err
}

// UpsertMovieByTMDBID inserts a movie snapshot or, when the tmdb_id already
// exists, replaces it wholesale. One statement covers the first insert, a
// staleness refresh, and two concurrent first requests racing — the loser's
// upsert lands on the winner's row instead of failing.
func UpsertMovieByTMDBID(ctx context.Context, tx db.Tx, m MovieRow) (MovieRow, error) {
	const q = `
INSERT INTO movies (tmdb_id, title, original_title, release_date, runtime_min,
                    overview, tagline, genres, poster_path, backdrop_path,
                    metadata_json, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (tmdb_id) DO UPDATE SET
    title          = excluded.title,
    original_title = excluded.original_title,
    release_date   = excluded.release_date,
    runtime_min    = excluded.runtime_min,
    overview       = excluded.overview,
    tagline        = excluded.tagline,
    genres         = excluded.genres,
    poster_path    = excluded.poster_path,
    backdrop_path  = excluded.backdrop_path,
    metadata_json  = excluded.metadata_json,
    fetched_at     = excluded.fetched_at
RETURNING ` + movieColumns
	return scanMovie(tx.QueryRow(ctx, q, m.TMDBID, m.Title, m.OriginalTitle, m.ReleaseDate,
		m.RuntimeMin, m.Overview, m.Tagline, m.GenresJSON, m.PosterPath,
		m.BackdropPath, m.MetadataJSON, m.FetchedAt))
}
