// Package ratings implements rating movies, shows and episodes (D-020).
package ratings

import "mbyte.ro/sofalog/features/video/common"

// Service is the ratings feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the ratings feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
