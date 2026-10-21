// Package upnext implements the "Up Next" home view: the next episode of every started show.
package upnext

import "mbyte.ro/sofalog/features/video/common"

// Service is the upnext feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the upnext feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
