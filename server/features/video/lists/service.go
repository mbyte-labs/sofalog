// Package lists implements the user's lists: the watchlist (with matching, D-028), favorites
// (D-030), the dropped-shows view and the Lists hub.
package lists

import "mbyte.ro/sofalog/features/video/common"

// Service is the lists feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the lists feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
