// Package copies implements owned copies (D-021): which formats a user holds a movie or season on.
package copies

import "mbyte.ro/sofalog/features/video/common"

// Service is the copies feature. The shared dependencies and catalog
// operations come from the embedded common.Core.
type Service struct {
	*common.Core
}

// NewService builds the copies feature on the shared core.
func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
