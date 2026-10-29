package lists

import "context"

// DroppedEntry is one card on the dropped page: the watchlist card's shape
// exactly, with TargetType always "show" and AddedAt the instant the show was
// dropped. Watched is false by construction — the stored status is 'dropped'.
type DroppedEntry = WatchlistEntry

// DroppedParams carries GET /video/dropped's query parameters as received;
// Dropped defaults and validates them. No type: every entry is a show.
type DroppedParams struct {
	OrderBy string // "" | added | title | release_date
	Order   string // "" | asc | desc
	Limit   string // positive integer; "" = unlimited
	Offset  string // non-negative integer; "" = 0
}

// Dropped is GET /video/dropped: one ordered, paged list of the shows this
// user has dropped, most recently dropped first by default, projected the way
// the watchlist projects its cards. A pure local read — never a provider call.
func (s *Service) Dropped(ctx context.Context, userID int64, p DroppedParams) ([]DroppedEntry, error) {
	page, err := parseListPage(p.OrderBy, p.Order, "", p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	targets, err := listDroppedShows(ctx, s.DB, droppedFilter{
		UserID:  userID,
		OrderBy: page.OrderBy,
		Desc:    page.Desc,
		Limit:   page.Limit,
		Offset:  page.Offset,
	})
	if err != nil {
		return nil, err
	}
	return s.projectListEntries(ctx, userID, targets)
}
