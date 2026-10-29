// Package stats is the video component's statistics feature: one read-only
// projection of the user's own watch log, ratings and show states, computed
// on the server in the user's zone (D-039). It imports only video/common.
package stats

import "mbyte.ro/sofalog/features/video/common"

type Service struct {
	*common.Core
}

func NewService(c *common.Core) *Service {
	return &Service{Core: c}
}
