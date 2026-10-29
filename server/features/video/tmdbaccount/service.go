// Package tmdbaccount implements the TMDB account link, its one-way sync outbox (D-029) and the
// setup pull (D-036).
package tmdbaccount

import (
	"context"
	"mbyte.ro/sofalog/features/video/common"
)

// Service is the tmdbaccount feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core

	// tmdbSync is the outbox worker (D-029), started by Run; nil on a
	// hand-built test Service. It drains the Core's TMDBSyncKick channel.
	tmdbSync *tmdbSyncRunner

	// tmdbPull holds each user's setup pull job (D-036): in memory, one per
	// user, the restore runner's shape. Cancelled by Run at shutdown.
	tmdbPull *tmdbPullRunner
}

// NewService builds the tmdbaccount feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c, tmdbSync: newTMDBSyncRunner(c.TMDBSyncKick), tmdbPull: newTMDBPullRunner()}
}

// Run starts the outbox worker — skipped on a hand-built Service with no
// runner — blocks until ctx ends, then cancels every running setup pull and
// waits for both to stop. A cancelled pull leaves the setup to be run again.
func (s *Service) Run(ctx context.Context) {
	if s.tmdbSync != nil {
		s.tmdbSync.wg.Add(1)
		go func() {
			defer s.tmdbSync.wg.Done()
			s.tmdbSyncLoop(ctx)
		}()
	}
	<-ctx.Done()
	if s.tmdbPull != nil {
		s.tmdbPull.cancelAll()
		s.tmdbPull.wg.Wait()
	}
	if s.tmdbSync != nil {
		s.tmdbSync.wg.Wait()
	}
}
