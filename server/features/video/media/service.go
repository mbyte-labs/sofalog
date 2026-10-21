// Package media implements the image proxies (posters, YouTube thumbnails, D-017) and serving
// a tracked item's stored artwork.
package media

import "mbyte.ro/sofalog/features/video/common"

// Service is the media feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the media feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
