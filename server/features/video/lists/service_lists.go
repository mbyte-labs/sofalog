package lists

import (
	"context"
	"errors"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// ListSummary is one card on the Lists hub: which list, how many movies and
// shows it holds, and a few backdrops from its most recently added items for
// the card's background. The SPA owns the list's title, icon and link, keyed
// by Key — the server only states the facts.
type ListSummary struct {
	Key    string `json:"key"` // watchlist | favorites | dropped
	Movies int    `json:"movies"`
	Shows  int    `json:"shows"`
	// Relative URLs of grid-sized backdrops (w300), most recently added first,
	// at most listArtMax. Items without a backdrop are skipped, so an empty
	// slice can mean an empty list or one whose items have no artwork.
	// Never null.
	Backdrops []string `json:"backdrops"`
	// The same backdrops at the full size (w1280), in the same order and of
	// the same length — the card offers each pair through srcset. Never null.
	BackdropsFull []string `json:"backdrops_full"`
}

// listArtMax is how many backdrops a card collages; listArtScan is how many of
// the newest entries are looked at to find them, so a few items without a
// backdrop do not leave the card bare.
const (
	listArtMax  = 4
	listArtScan = 12
)

// Lists summarises every list this user has, in a fixed order: the watchlist,
// the favorites (D-030), then the dropped shows. All three always appear, with
// zero counts when empty.
// A pure local read — every listed item is tracked, so its backdrop is on disk
// or, failing that, behind the cache proxy; never a provider call.
func (s *Service) Lists(ctx context.Context, userID int64) ([]ListSummary, error) {
	counts, err := countListTargets(ctx, s.DB, userID)
	if err != nil {
		return nil, err
	}

	// The newest entries, through the list pages' own queries, so "most
	// recently added" has one definition.
	watchlist, err := listWatchlistTargets(ctx, s.DB, watchlistFilter{
		UserID: userID, OrderBy: "added", Type: "all", Desc: true, Limit: listArtScan,
	})
	if err != nil {
		return nil, err
	}
	favorites, err := listFavoriteTargets(ctx, s.DB, favoritesFilter{
		UserID: userID, OrderBy: "added", Type: "all", Desc: true, Limit: listArtScan,
	})
	if err != nil {
		return nil, err
	}
	dropped, err := listDroppedShows(ctx, s.DB, droppedFilter{
		UserID: userID, OrderBy: "added", Desc: true, Limit: listArtScan,
	})
	if err != nil {
		return nil, err
	}

	out := []ListSummary{{Key: "watchlist"}, {Key: "favorites"}, {Key: "dropped"}}
	for i, targets := range [][]watchlistTarget{watchlist, favorites, dropped} {
		for _, c := range counts {
			if c.List != out[i].Key {
				continue
			}
			if c.TargetType == "show" {
				out[i].Shows = c.N
			} else {
				out[i].Movies = c.N
			}
		}
		out[i].Backdrops, out[i].BackdropsFull, err = s.listBackdrops(ctx, targets)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// listBackdrops picks up to listArtMax backdrops out of targets, in order, at
// both sizes (parallel slices), through the same linker the details views
// use: the stored file when the asset set holds it, else the cache proxy.
func (s *Service) listBackdrops(ctx context.Context, targets []watchlistTarget) (thumbs, fulls []string, err error) {
	// The empty literals keep the contract arrays — never encode null.
	thumbs = make([]string, 0, listArtMax)
	fulls = make([]string, 0, listArtMax)
	for _, t := range targets {
		if len(thumbs) == listArtMax {
			break
		}
		var thumb, full *string
		if t.TargetType == "show" {
			row, err := common.GetShowByID(ctx, s.DB, t.TargetID)
			if errors.Is(err, httpx.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, nil, err
			}
			thumb, full = common.ShowLinker(row.ID, s.ShowFiles(row.ID)).Backdrop(row.BackdropPath)
		} else {
			row, err := common.GetMovieByID(ctx, s.DB, t.TargetID)
			if errors.Is(err, httpx.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, nil, err
			}
			thumb, full = common.MovieLinker(row.ID, s.MovieFiles(row.ID)).Backdrop(row.BackdropPath)
		}
		if thumb == nil {
			continue
		}
		thumbs = append(thumbs, *thumb)
		fulls = append(fulls, *full)
	}
	return thumbs, fulls, nil
}
