// Package people implements the person details view (D-027).
package people

import "mbyte.ro/sofalog/features/video/common"

// Service is the people feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the people feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
