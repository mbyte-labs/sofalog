package videotest

import (
	"context"
	"strconv"
	"testing"

	"mbyte.ro/sofalog/db"
	"mbyte.ro/sofalog/features/video/common"
)

func ShowPath(showID int64) string { return strconv.FormatInt(showID, 10) }

func PtrInt(v int) *int { return &v }

const (
	TMDBSessionNewBody = `{"success":true,"session_id":"sess-1"}`
)

const (
	TMDBAccountBody = `{"id":42,"username":"bob"}`
)

type SyncRowView struct {
	Kind     string
	Op       string
	Rating   *int
	Attempts int
	Retry    *string
}

func SyncRows(t *testing.T, database *db.DB, userID int64) map[string]SyncRowView {
	t.Helper()
	rows, err := database.Read(context.Background(),
		`SELECT kind, target_type, target_id, op, rating, attempts, next_retry_at FROM user_tmdb_sync WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		t.Fatalf("reading outbox: %v", err)
	}
	defer rows.Close()
	out := map[string]SyncRowView{}
	for rows.Next() {
		var tt string
		var id int64
		var v SyncRowView
		if err := rows.Scan(&v.Kind, &tt, &id, &v.Op, &v.Rating, &v.Attempts, &v.Retry); err != nil {
			t.Fatalf("scanning outbox: %v", err)
		}
		// Rating rows keep the bare key so the existing assertions read as
		// before; watchlist and favorite rows are prefixed with their kind.
		key := tt + ":" + JSONNumber(id)
		if v.Kind == common.SyncKindWatchlist || v.Kind == common.SyncKindFavorite {
			key = v.Kind + "/" + key
		}
		out[key] = v
	}
	return out
}

func PtrInt64(v int64) *int64 { return &v }

// CountRows is a raw-SQL test probe (the layering grep excludes _test.go).
func CountRows(t *testing.T, database *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.ReadRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	return n
}

func MovieStatus(t *testing.T, database *db.DB, userID, movieID int64) string {
	t.Helper()
	var status string
	err := database.ReadRow(context.Background(),
		`SELECT status FROM user_movie_states WHERE user_id = ? AND movie_id = ?`,
		userID, movieID).Scan(&status)
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	return status
}
