// Package watches implements marking things watched: single and dated watches, the bulk show/season
// marks, watch deletion, and dropping / un-dropping a show.
package watches

import "mbyte.ro/sofalog/features/video/common"

// Service is the watches feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the watches feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
