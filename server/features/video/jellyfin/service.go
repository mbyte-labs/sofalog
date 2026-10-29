// Package jellyfin implements the Jellyfin library scan (D-033).
package jellyfin

import (
	"context"
	"mbyte.ro/sofalog/features/video/common"
)

// Service is the jellyfin feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core

	// jellyfinScan is the scan loop (D-033), started by Run only when a server
	// is configured.
	jellyfinScan *jellyfinRunner
}

// NewService builds the jellyfin feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c, jellyfinScan: newJellyfinRunner(c.JellyfinEvery)}
}

// Run starts the library scan loop — only with a server configured, and
// skipped on a hand-built Service with no runner — and blocks until ctx ends
// and the loop has stopped.
func (s *Service) Run(ctx context.Context) {
	if s.jellyfinScan == nil || !s.JellyfinSynced() {
		<-ctx.Done()
		return
	}
	s.jellyfinScan.wg.Add(1)
	go func() {
		defer s.jellyfinScan.wg.Done()
		s.jellyfinLoop(ctx)
	}()
	<-ctx.Done()
	s.jellyfinScan.wg.Wait()
}
