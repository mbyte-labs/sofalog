// Package details implements the movie, show, season and episode details views (D-015), with
// their similar titles (D-024) and streaming sections (D-022).
package details

import "mbyte.ro/sofalog/features/video/common"

// Service is the details feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the details feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
