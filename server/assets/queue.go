package assets

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mbyte.ro/sofalog/db"
)

// Fetcher downloads one image given its provider-relative source ("w500/x.jpg"
// for tmdb, a video key for youtube). main injects one per provider, closing
// over the shared clients — their per-provider rate limiters are the budget
// the queue rides on.
type Fetcher func(ctx context.Context, sourceURL string) ([]byte, error)

const (
	// maxAttempts is the retry budget before a job is terminally 'failed'. A
	// later sync of the same item re-enqueues failed jobs, so a TMDB outage
	// costs at most one refresh interval, not the artwork.
	maxAttempts = 8
	// retryBase doubles per attempt, capped at retryMax.
	retryBase = 30 * time.Second
	retryMax  = time.Hour
	// interJobPause spaces downloads out. Together with the providers' own
	// rate limiters this is the whole throttle — a 60-image movie drains in
	// under a minute without hammering anyone.
	interJobPause = 250 * time.Millisecond
	// idlePoll is the fallback wake-up when no Kick arrives — it is what makes
	// backoff retries happen with nothing else going on.
	idlePoll = 30 * time.Second
)

// Queue is the download worker over asset_jobs: durable (rows, not memory),
// idempotent (dest_path identity), resumable (ResetRunning + re-claim), with
// bounded backoff retries. One goroutine drains it; it never initiates
// provider traffic on its own — it only downloads what user actions enqueued.
type Queue struct {
	db    *db.DB
	tree  *Tree
	cache *Cache
	fetch map[string]Fetcher
	kick  chan struct{}
	now   func() time.Time
}

// NewQueue wires a queue. Run must be started for jobs to drain.
func NewQueue(database *db.DB, tree *Tree, cache *Cache, fetchers map[string]Fetcher) *Queue {
	return &Queue{
		db:    database,
		tree:  tree,
		cache: cache,
		fetch: fetchers,
		kick:  make(chan struct{}, 1),
		now:   time.Now,
	}
}

// Kick wakes the worker after an enqueue. Non-blocking: one pending wake-up
// is enough, the worker drains everything it finds.
func (q *Queue) Kick() {
	select {
	case q.kick <- struct{}{}:
	default:
	}
}

// Run drains the queue until ctx is cancelled. Start it as `go queue.Run(ctx)`
// from main, after ResetRunning.
func (q *Queue) Run(ctx context.Context) {
	for {
		job, ok, err := claimNext(ctx, q.db, q.now().UTC().Format(time.RFC3339))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("asset queue: claiming a job", "err", err)
			ok = false
		}
		if !ok {
			select {
			case <-q.kick:
			case <-time.After(idlePoll):
			case <-ctx.Done():
				return
			}
			continue
		}

		// The pause is skipped for a job served out of data/cache: it exists
		// to throttle provider traffic, and a promotion made no request. That
		// matters here because a user has usually browsed a show's pages
		// before tracking it, so a meaningful slice of the queued set is
		// already cached — and it makes a re-track after a D-008 delete drain
		// immediately.
		if fetched := q.process(ctx, job); !fetched {
			select {
			case <-ctx.Done():
				return
			default:
			}
			continue
		}

		select {
		case <-time.After(interJobPause):
		case <-ctx.Done():
			return
		}
	}
}

// process resolves one claimed job: promote the bytes from data/cache when the
// image proxy already fetched them (SPEC §5 — promotion, not re-download),
// otherwise download, then write into the tree and settle the row.
//
// It reports whether the job actually reached the provider, which is what
// decides whether the caller pauses: interJobPause throttles PROVIDER traffic,
// and a promotion makes no provider request at all.
func (q *Queue) process(ctx context.Context, job claimedJob) (fetched bool) {
	data, fetched, err := q.resolve(ctx, job)
	if err == nil {
		err = q.tree.Write(data, job.DestPath)
	}
	if err == nil {
		if err := markDone(ctx, q.db, job.ID); err != nil {
			slog.Error("asset queue: marking done", "dest", job.DestPath, "err", err)
		}
		return fetched
	}
	if ctx.Err() != nil {
		// Shutdown, not failure: ResetRunning re-queues the job at startup.
		return fetched
	}

	attempts := job.Attempts + 1
	terminal := attempts >= maxAttempts
	backoff := min(retryBase<<(attempts-1), retryMax)
	retryAt := q.now().UTC().Add(backoff).Format(time.RFC3339)
	slog.Warn("asset queue: job failed", "dest", job.DestPath, "attempts", attempts,
		"terminal", terminal, "err", err)
	if err := markFailed(ctx, q.db, job.ID, attempts, err.Error(), retryAt, terminal); err != nil {
		slog.Error("asset queue: marking failed", "dest", job.DestPath, "err", err)
	}
	return fetched
}

// resolve produces the job's bytes: cache first, provider second. fetched
// reports whether the provider was actually reached — false for a promotion out
// of data/cache, which is a disk read and throttles nothing.
func (q *Queue) resolve(ctx context.Context, job claimedJob) (data []byte, fetched bool, err error) {
	if key := cacheKeyFor(job.Job); key != nil {
		data, ok, err := q.cache.Read(key...)
		if err != nil {
			slog.Warn("asset queue: cache read failed, downloading instead",
				"dest", job.DestPath, "err", err)
		} else if ok {
			return data, false, nil
		}
	}
	fetch, ok := q.fetch[job.Provider]
	if !ok {
		return nil, false, fmt.Errorf("no fetcher for provider %q", job.Provider)
	}
	data, err = fetch(ctx, job.SourceURL)
	return data, true, err
}

// cacheKeyFor maps a job to the cache key the lazy image proxies write, or nil
// when the provider has no proxy-compatible key. The mapping mirrors
// video's poster and youtube-thumb proxies — same tree, same names.
func cacheKeyFor(job Job) []string {
	switch job.Provider {
	case "tmdb":
		size, file, ok := strings.Cut(job.SourceURL, "/")
		if !ok {
			return nil
		}
		return []string{"tmdb", "posters", size, file}
	case "youtube":
		return []string{"youtube", "thumbs", job.SourceURL + ".jpg"}
	default:
		return nil
	}
}
