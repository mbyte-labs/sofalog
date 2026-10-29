package assets

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"mbyte.ro/sofalog/db"
)

// Job is one download the queue owes: a single provider image landing at a
// single destination file. DestPath is the identity (UNIQUE) — enqueueing the
// same file twice is a no-op, which is what makes the queue resumable.
type Job struct {
	Provider  string // tmdb | youtube (steam joins later)
	MediaType string
	ItemID    int64
	Kind      string
	Size      string // thumb | full | single
	// SourceURL is a provider-relative identifier ("w500/abc.jpg", a YouTube
	// key), never an absolute URL — the image host stays runtime-resolved.
	SourceURL string
	DestPath  string // from paths.go, relative to the data dir
}

// claimedJob is a Job the worker has taken, with its row identity and attempt
// count for the retry bookkeeping.
type claimedJob struct {
	ID       int64
	Attempts int
	Job
}

// UpsertJobs enqueues the given jobs. A job already pending or running with
// the same source is left untouched (its retry budget survives); one whose
// source changed, or that already finished (done or failed), is reset to
// pending with a fresh budget. Resetting finished jobs is deliberate: callers
// only enqueue files missing on disk, so a done row arriving here means its
// file vanished and re-downloading is the healing move.
func UpsertJobs(ctx context.Context, tx db.Tx, jobs []Job, now string) error {
	const q = `
INSERT INTO asset_jobs (provider, media_type, item_id, kind, size, source_url,
                        dest_path, status, attempts, last_error, next_retry_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', 0, NULL, NULL, ?)
ON CONFLICT (dest_path) DO UPDATE SET
    provider      = excluded.provider,
    source_url    = excluded.source_url,
    status        = 'pending',
    attempts      = 0,
    last_error    = NULL,
    next_retry_at = NULL
WHERE asset_jobs.source_url <> excluded.source_url
   OR asset_jobs.status IN ('done', 'failed')`
	for _, j := range jobs {
		if _, err := tx.Exec(ctx, q, j.Provider, j.MediaType, j.ItemID, j.Kind,
			j.Size, j.SourceURL, j.DestPath, now); err != nil {
			return err
		}
	}
	return nil
}

// DeleteJobsForItemDests drops the named jobs of one item — cancelling stale
// pending downloads and clearing bookkeeping for artwork a refresh removed.
//
// This is the chunkable inverse of "delete everything not in the keep set", and
// it is deliberately the inverse. ListJobSources already returns every one of an
// item's rows, so the caller can compute `sources − desired` in Go, which is
// exactly equivalent — and a NOT IN over the KEEP set does not scale: a show's
// keep set runs to hundreds of paths and an 870-episode show to roughly 1800,
// past the 999 bind-parameter ceiling older SQLite builds still ship with, and
// an oversized statement on any build. The delete set, by contrast, is usually
// empty, so the common no-op sync runs no statement at all.
//
// It also makes the job diff symmetric with the disk diff, which the caller
// already computes in Go.
func DeleteJobsForItemDests(ctx context.Context, tx db.Tx, mediaType string, itemID int64, dests []string) error {
	// Chunked well below the lowest ceiling, so one oversized item can never
	// produce one oversized statement.
	const chunk = 400
	for len(dests) > 0 {
		n := min(chunk, len(dests))
		batch := dests[:n]
		dests = dests[n:]

		q := `DELETE FROM asset_jobs WHERE media_type = ? AND item_id = ? AND dest_path IN (` +
			strings.Repeat("?, ", len(batch)-1) + `?)`
		args := make([]any, 0, len(batch)+2)
		args = append(args, mediaType, itemID)
		for _, d := range batch {
			args = append(args, d)
		}
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

// ListJobSources returns dest_path → source_url for one item's jobs — the
// bookkeeping half of the sync diff, which re-enqueues a file whose source
// moved even when its (now outdated) bytes are still on disk.
func ListJobSources(ctx context.Context, d *db.DB, mediaType string, itemID int64) (map[string]string, error) {
	const q = `SELECT dest_path, source_url FROM asset_jobs WHERE media_type = ? AND item_id = ?`
	rows, err := d.Read(ctx, q, mediaType, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var dest, source string
		if err := rows.Scan(&dest, &source); err != nil {
			return nil, err
		}
		out[dest] = source
	}
	return out, rows.Err()
}

// ResetRunning returns crashed-mid-download jobs to the queue at startup: a
// row can only be 'running' while a worker holds it, and workers do not
// survive the process.
func ResetRunning(ctx context.Context, d *db.DB) error {
	return d.InTx(ctx, func(tx db.Tx) error {
		const q = `UPDATE asset_jobs SET status = 'pending' WHERE status = 'running'`
		_, err := tx.Exec(ctx, q)
		return err
	})
}

// claimNext atomically takes the oldest due pending job. ok=false means the
// queue is drained (or everything is waiting out a backoff).
func claimNext(ctx context.Context, d *db.DB, now string) (claimedJob, bool, error) {
	const q = `
UPDATE asset_jobs SET status = 'running'
WHERE id = (SELECT id FROM asset_jobs
            WHERE status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= ?)
            ORDER BY id LIMIT 1)
RETURNING id, attempts, provider, media_type, item_id, kind, size, source_url, dest_path`
	var j claimedJob
	err := d.InTx(ctx, func(tx db.Tx) error {
		return tx.QueryRow(ctx, q, now).Scan(&j.ID, &j.Attempts, &j.Provider,
			&j.MediaType, &j.ItemID, &j.Kind, &j.Size, &j.SourceURL, &j.DestPath)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return claimedJob{}, false, nil
	}
	if err != nil {
		return claimedJob{}, false, err
	}
	return j, true, nil
}

// markDone records a completed download. Only a still-running row flips: if a
// sync reset the job mid-download (the source moved), the reset wins and the
// fresh source gets re-fetched instead of stale bytes being recorded as done.
func markDone(ctx context.Context, d *db.DB, id int64) error {
	return d.InTx(ctx, func(tx db.Tx) error {
		const q = `
UPDATE asset_jobs SET status = 'done', last_error = NULL, next_retry_at = NULL
WHERE id = ? AND status = 'running'`
		_, err := tx.Exec(ctx, q, id)
		return err
	})
}

// markFailed records a failed attempt: back to pending with a retry time, or
// terminally failed once the attempt budget is spent. lastError is kept for
// diagnosis; a permanently 404ing image must not spin forever.
func markFailed(ctx context.Context, d *db.DB, id int64, attempts int, lastError, retryAt string, terminal bool) error {
	// The status='running' guard mirrors markDone: a job a sync reset
	// mid-download keeps its fresh budget instead of inheriting the failure
	// of a fetch for the source it no longer has.
	return d.InTx(ctx, func(tx db.Tx) error {
		if terminal {
			const q = `
UPDATE asset_jobs SET status = 'failed', attempts = ?, last_error = ?, next_retry_at = NULL
WHERE id = ? AND status = 'running'`
			_, err := tx.Exec(ctx, q, attempts, lastError, id)
			return err
		}
		const q = `
UPDATE asset_jobs SET status = 'pending', attempts = ?, last_error = ?, next_retry_at = ?
WHERE id = ? AND status = 'running'`
		_, err := tx.Exec(ctx, q, attempts, lastError, retryAt, id)
		return err
	})
}
