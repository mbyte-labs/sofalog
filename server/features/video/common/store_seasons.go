package common

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/httpx"
)

// SeasonRow mirrors the seasons catalog table. Nullable columns are pointers.
// The addressing key is (ShowTMDBID, SeasonNumber).
//
// ShowID is the one field that does NOT mirror a nullable column — seasons.show_id
// is NOT NULL. It is a pointer because this struct doubles as the carrier for a
// season fetched live and never stored, which is what an untracked show's season
// view returns (D-015): nil there means "no row", the same thing ID == 0 means.
// Anything that builds an asset path from a season needs it non-nil, because
// those paths are rooted at the show's LOCAL id.
type SeasonRow struct {
	ID           int64
	ShowID       *int64
	ShowTMDBID   int64
	SeasonNumber int
	Name         string
	ShowTitle    string
	AirDate      *string
	EpisodeCount int
	Overview     *string
	PosterPath   *string
	MetadataJSON string
	FetchedAt    string
}

const seasonColumns = `id, show_id, show_tmdb_id, season_number, name, show_title,
air_date, episode_count, overview, poster_path, metadata_json, fetched_at`

func scanSeason(row *sql.Row) (SeasonRow, error) {
	var s SeasonRow
	err := row.Scan(&s.ID, &s.ShowID, &s.ShowTMDBID, &s.SeasonNumber, &s.Name,
		&s.ShowTitle, &s.AirDate, &s.EpisodeCount, &s.Overview, &s.PosterPath,
		&s.MetadataJSON, &s.FetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SeasonRow{}, fmt.Errorf("%w: no such season", httpx.ErrNotFound)
	}
	if err != nil {
		return SeasonRow{}, err
	}
	return s, nil
}

func GetSeasonByID(ctx context.Context, d *db.DB, id int64) (SeasonRow, error) {
	const q = `SELECT ` + seasonColumns + ` FROM seasons WHERE id = ?`
	return scanSeason(d.ReadRow(ctx, q, id))
}

func GetSeasonByTMDBKey(ctx context.Context, d *db.DB, showTMDBID int64, seasonNumber int) (SeasonRow, error) {
	const q = `SELECT ` + seasonColumns + ` FROM seasons WHERE show_tmdb_id = ? AND season_number = ?`
	return scanSeason(d.ReadRow(ctx, q, showTMDBID, seasonNumber))
}

// SeasonIDsByShow maps a show's season numbers to their local season ids, for
// the show details view's season links. An UNTRACKED show has no season rows at
// all — only tracking writes them (D-015) — so it returns an empty map, and the
// show page links its seasons by (tmdb_id, season_number) instead. Never an
// error.
func SeasonIDsByShow(ctx context.Context, d *db.DB, showTMDBID int64) (map[int]int64, error) {
	const q = `SELECT season_number, id FROM seasons WHERE show_tmdb_id = ?`
	rows, err := d.Read(ctx, q, showTMDBID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[int]int64)
	for rows.Next() {
		var number int
		var id int64
		if err := rows.Scan(&number, &id); err != nil {
			return nil, err
		}
		ids[number] = id
	}
	return ids, rows.Err()
}

// UpsertSeasonByTMDBKey inserts a season snapshot or, when the season already
// exists, replaces it wholesale. One statement covers the first materialise, a
// staleness refresh, and two concurrent views racing — the loser's upsert lands
// on the winner's row instead of failing.
//
// ShowID is a parameter rather than a field read off s because it is the rule
// this table exists under: a season row is only ever written beneath a TRACKED
// show (D-015), so the link is always known at insert time and can never be
// NULL. Taking it explicitly makes that a compile-time obligation of every
// caller instead of a runtime constraint violation.
func UpsertSeasonByTMDBKey(ctx context.Context, tx db.Tx, showID int64, s SeasonRow) (SeasonRow, error) {
	const q = `
INSERT INTO seasons (show_id, show_tmdb_id, season_number, name, show_title,
                     air_date, episode_count, overview, poster_path,
                     metadata_json, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (show_tmdb_id, season_number) DO UPDATE SET
    show_id       = excluded.show_id,
    name          = excluded.name,
    show_title    = excluded.show_title,
    air_date      = excluded.air_date,
    episode_count = excluded.episode_count,
    overview      = excluded.overview,
    poster_path   = excluded.poster_path,
    metadata_json = excluded.metadata_json,
    fetched_at    = excluded.fetched_at
RETURNING ` + seasonColumns
	return scanSeason(tx.QueryRow(ctx, q, showID, s.ShowTMDBID, s.SeasonNumber,
		s.Name, s.ShowTitle, s.AirDate, s.EpisodeCount, s.Overview, s.PosterPath,
		s.MetadataJSON, s.FetchedAt))
}

// seasonPosterPaths maps a tracked show's season numbers to their poster paths,
// for the asset diff. Columns, not metadata_json — the same reasoning as
// episodeStillPaths.
func seasonPosterPaths(ctx context.Context, d *db.DB, showID int64) (map[int]string, error) {
	const q = `SELECT season_number, poster_path FROM seasons
WHERE show_id = ? AND poster_path IS NOT NULL`
	rows, err := d.Read(ctx, q, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	posters := make(map[int]string)
	for rows.Next() {
		var number int
		var path string
		if err := rows.Scan(&number, &path); err != nil {
			return nil, err
		}
		posters[number] = path
	}
	return posters, rows.Err()
}

// seasonSnapshotJSON returns one season's stored snapshot body, for the callers
// that need a field the seasons columns do not carry — an episode's community
// score, which lives in the season document because episodes deliberately store
// no metadata_json of their own.
func seasonSnapshotJSON(ctx context.Context, d *db.DB, showID int64, seasonNumber int) (string, error) {
	const q = `SELECT metadata_json FROM seasons WHERE show_id = ? AND season_number = ?`
	var raw string
	err := d.ReadRow(ctx, q, showID, seasonNumber).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: no such season", httpx.ErrNotFound)
	}
	return raw, err
}
