package common

import (
	"fmt"

	"mbyte.ro/sofalog/httpx"
)

// AssertRatingInRange is the 1-10 bound, stated once. MarkWatched shares it:
// a watch logged with a rating goes through the same gate as PUT /video/ratings,
// so the two cannot disagree about what the column's CHECK will accept.
func AssertRatingInRange(rating int) error {
	if rating < 1 || rating > 10 {
		return fmt.Errorf("%w: rating must be an integer between 1 and 10", httpx.ErrValidation)
	}
	return nil
}
