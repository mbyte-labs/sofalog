// Package upcoming implements the upcoming calendar (D-026): dated episodes and movies a user is waiting for.
package upcoming

import "mbyte.ro/sofalog/features/video/common"

// Service is the upcoming feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the upcoming feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
