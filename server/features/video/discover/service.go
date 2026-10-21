// Package discover implements search and the trending / discover lists (D-034).
package discover

import "mbyte.ro/sofalog/features/video/common"

// Service is the discover feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the discover feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
