// Package restore implements restoring a Ticket Box backup (D-023).
package restore

import (
	"context"
	"mbyte.ro/sofalog/features/video/common"
)

// Service is the restore feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core

	// restore holds each user's backup-restore job (D-023): in memory, one
	// per user, alive until the next start or the process ends.
	restore *restoreRunner
}

// NewService builds the restore feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c, restore: newRestoreRunner()}
}

// Run blocks until ctx ends, then cancels every running restore and waits
// for them to stop. A cancelled restore fails with code "cancelled": the
// groups it had committed stay, the one in flight rolls back with its
// transaction, and the user runs the file again.
func (s *Service) Run(ctx context.Context) {
	<-ctx.Done()
	s.restore.cancelAll()
	s.restore.wg.Wait()
}
