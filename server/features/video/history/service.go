// Package history implements the watch history feed.
package history

import "mbyte.ro/sofalog/features/video/common"

// Service is the history feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the history feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
