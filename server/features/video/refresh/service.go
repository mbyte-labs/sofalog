// Package refresh implements the scheduled TMDB refresh and the status sweep (D-006, D-025).
package refresh

import (
	"context"
	"mbyte.ro/sofalog/features/video/common"
)

// Service is the refresh feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core

	// refresh is the scheduled TMDB refresh (D-006, D-025): two jobs on one
	// hourly loop, started by Run. Nil on a Service built by hand in a test,
	// which is also what keeps Run from starting a loop against a nil tmdb.
	refresh *refreshRunner
}

// NewService builds the refresh feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c, refresh: newRefreshRunner(c.RefreshEvery, c.ArchiveEvery)}
}

// Run starts the scheduled refresh loop — skipped on a hand-built Service
// with no runner — and blocks until ctx ends and the loop has stopped. A pass
// cut short simply resumes on the next start: the row timestamps are its only
// state.
func (s *Service) Run(ctx context.Context) {
	if s.refresh == nil {
		<-ctx.Done()
		return
	}
	s.refresh.wg.Add(1)
	go func() {
		defer s.refresh.wg.Done()
		s.refreshLoop(ctx)
	}()
	<-ctx.Done()
	s.refresh.wg.Wait()
}
