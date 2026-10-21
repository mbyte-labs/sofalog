package restore

import (
	"context"

	"mbyte.ro/sofalog/db"
)

// The restore's replace step: a backup names a movie or a show, and the user's
// existing watch rows for it make way for the file's. Both predicates carry
// target_type (D-013) and user_id, and both report how many rows went so the
// report can say "replaced N". Nothing here writes pending_deletions — the
// catalog row and its artwork stay, exactly as deleteWatchByID leaves them.

// deleteMovieWatches removes this user's watch rows for one movie.
func deleteMovieWatches(ctx context.Context, tx db.Tx, userID, movieID int64) (int64, error) {
	const q = `
DELETE FROM user_watch_history
WHERE user_id = ? AND target_type = 'movie' AND target_id = ?`
	res, err := tx.Exec(ctx, q, userID, movieID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// deleteShowEpisodeWatches removes this user's episode watch rows across one
// whole show — every season, specials included. The subquery resolves the
// polymorphic target through episodes.show_id, which is the only way an
// episode row knows its show.
func deleteShowEpisodeWatches(ctx context.Context, tx db.Tx, userID, showID int64) (int64, error) {
	const q = `
DELETE FROM user_watch_history
WHERE user_id = ? AND target_type = 'episode'
  AND target_id IN (SELECT id FROM episodes WHERE show_id = ?)`
	res, err := tx.Exec(ctx, q, userID, showID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// episodeKey is an episode's position within its show — the handle every
// other path addresses an episode by, since sofalog stores no episode TMDB ids.
type episodeKey struct {
	Season, Episode int
}

// episodeIDsByShow maps every episode of one show by position to its local id.
// Read pool: the show's materialisation has already committed by the time the
// restore needs this. A show with no episode rows yields an empty map, never
// an error — the caller reports each miss as a skipped entry.
func episodeIDsByShow(ctx context.Context, d *db.DB, showID int64) (map[episodeKey]int64, error) {
	const q = `
SELECT season_number, episode_number, id
FROM episodes
WHERE show_id = ?`
	rows, err := d.Read(ctx, q, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make(map[episodeKey]int64)
	for rows.Next() {
		var k episodeKey
		var id int64
		if err := rows.Scan(&k.Season, &k.Episode, &id); err != nil {
			return nil, err
		}
		ids[k] = id
	}
	return ids, rows.Err()
}

// seasonDocument is one stored season's number and its TMDB snapshot, whose
// episodes array still carries TMDB's own episode ids.
type seasonDocument struct {
	SeasonNumber int
	MetadataJSON string
}

// seasonDocumentsByShow reads every stored season document of one show. The
// restore decodes them to resolve a backup's episode TMDB ids — the episodes
// table deliberately has no such column (see its schema comment), so the
// season snapshot is the only place sofalog still holds them.
func seasonDocumentsByShow(ctx context.Context, d *db.DB, showID int64) ([]seasonDocument, error) {
	const q = `
SELECT season_number, metadata_json
FROM seasons
WHERE show_id = ?`
	rows, err := d.Read(ctx, q, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []seasonDocument
	for rows.Next() {
		var doc seasonDocument
		if err := rows.Scan(&doc.SeasonNumber, &doc.MetadataJSON); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}
