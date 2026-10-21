package upcoming

import (
	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/features/video/videotest"
)

// testService carries no DB at all: parameter validation must reject before
// anything could reach the store, and a nil pointer proves it does.
func testService() *Service {
	return &Service{Core: &common.Core{Now: videotest.FixedClock}}
}
