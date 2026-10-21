package upnext

import (
	"context"
	"fmt"
	"strconv"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// UpNextEntry is one card on the "Up Next To Watch" page: the next unwatched
// episode of a started show plus that show's derived progress. Nullable fields
// are pointers without omitempty — the SPA distinguishes null from absent.
type UpNextEntry struct {
	Show UpNextShow `json:"show"`
	// Relative asset paths, or null together: the poster at the grid size
	// (w185) and at the full size (w500), offered together through srcset.
	Poster     *string       `json:"poster"`
	PosterFull *string       `json:"poster_full"`
	Episode    UpNextEpisode `json:"episode"`
	// When this user last watched any episode of the show. RFC3339 UTC —
	// MAX(watched_at) over the log, so it carries the same precision as its
	// source and the client can render a time of day, not just a day. Only
	// started shows appear, so it is never null in practice; the pointer keeps
	// the published nullable shape.
	LastWatchedAt *string        `json:"last_watched_at"`
	Progress      UpNextProgress `json:"progress"`
}

type UpNextShow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type UpNextEpisode struct {
	ID int64 `json:"id"`
	// SeasonID is the local seasons.id, and the only handle here that
	// addresses anything: an episode has no id-addressed endpoint, so its
	// details page is reached by this plus Number — the same pair
	// HistoryEpisode carries. Null only if the season row has gone, which the
	// catalog write order makes unreachable in practice.
	SeasonID *int64  `json:"season_id"`
	Season   int     `json:"season"`
	Number   int     `json:"number"`
	Title    string  `json:"title"`
	AirDate  *string `json:"air_date"` // YYYY-MM-DD, or null when undated
	// TMDB community score (0–10) from the stored metadata snapshot, not a
	// user rating — up-next episodes are by definition unwatched.
	Rating *float64 `json:"rating"`
	Marker *string  `json:"marker"` // season_premiere | mid_season_finale | season_finale | null
	// Whether the episode has aired, computed server-side against the user's today (D-038) —
	// false for a future or unknown air date. The card renders its "Upcoming"
	// badge from this; the client never derives it.
	Aired bool `json:"aired"`
}

type UpNextProgress struct {
	Watched    int `json:"watched"`
	AiredTotal int `json:"aired_total"`
	Total      int `json:"total"`
}

// UpNextParams carries the query parameters as received; UpNext defaults and
// validates them so the handler stays free of domain branching.
type UpNextParams struct {
	OrderBy             string // "" | last_watched | show_name | air_date
	Order               string // "" | asc | desc
	OnlyWithReleaseDate string // "" or a boolean literal
	OnlyReleased        string // "" or a boolean literal
}

// UpNext lists, per show the user has started (≥1 episode watch, not dropped),
// the first unwatched non-special episode strictly after their highest watched
// position — the derivation lives in listUpNext. A caught-up show is simply
// absent; the response is a bare array either way.
func (s *Service) UpNext(ctx context.Context, userID int64, p UpNextParams) ([]UpNextEntry, error) {
	orderBy := p.OrderBy
	if orderBy == "" {
		orderBy = "last_watched"
	}
	switch orderBy {
	case "last_watched", "show_name", "air_date":
	default:
		return nil, fmt.Errorf("%w: order_by must be one of last_watched, show_name, air_date", httpx.ErrValidation)
	}

	order := p.Order
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return nil, fmt.Errorf("%w: order must be asc or desc", httpx.ErrValidation)
	}

	onlyWithReleaseDate, err := parseBoolParam("only_with_release_date", p.OnlyWithReleaseDate)
	if err != nil {
		return nil, err
	}
	onlyReleased, err := parseBoolParam("only_released", p.OnlyReleased)
	if err != nil {
		return nil, err
	}

	// One clock reading for the whole request: the only_released filter, the
	// aired flag and the progress counts must agree on where "aired" ends —
	// and it ends at midnight in this user's zone (D-038).
	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	today := clk.Today

	rows, err := listUpNext(ctx, s.DB, upNextFilter{
		UserID:              userID,
		OrderBy:             orderBy,
		Desc:                order == "desc",
		OnlyWithReleaseDate: onlyWithReleaseDate,
		OnlyReleased:        onlyReleased,
		Today:               today,
	})
	if err != nil {
		return nil, err
	}

	// One snapshot parse per (show, season), not per entry — the same memo the
	// history feed uses for its ratings.
	snaps := map[common.SeasonKey]map[int]provider.TMDBTVSeasonEpisode{}
	// The empty literal keeps the contract a bare array — never encode null.
	entries := make([]UpNextEntry, 0, len(rows))
	for _, r := range rows {
		entry := UpNextEntry{
			Show: UpNextShow{ID: r.ShowID, Name: r.ShowTitle},
			Episode: UpNextEpisode{
				ID:       r.EpisodeID,
				SeasonID: r.SeasonID,
				Season:   r.SeasonNumber,
				Number:   r.EpisodeNumber,
				AirDate:  r.AirDate,
				Aired:    r.AirDate != nil && *r.AirDate <= today,
			},
		}
		if r.EpisodeTitle != nil {
			entry.Episode.Title = *r.EpisodeTitle
		}
		lastWatched := r.LastWatchedAt
		entry.LastWatchedAt = &lastWatched

		// The card's poster is the SEASON's, falling back to the show's, both
		// through the asset linker — exactly the history feed's choice, so a
		// tracked show renders from disk with TMDB down.
		link := common.ShowLinker(r.ShowID, s.ShowFiles(r.ShowID))
		switch {
		case r.SeasonPosterPath != nil:
			entry.Poster, entry.PosterFull = link.SeasonPoster(r.SeasonNumber, r.SeasonPosterPath)
		case r.ShowPosterPath != nil:
			entry.Poster, entry.PosterFull = link.Poster(r.ShowPosterPath)
		}

		// Community score and marker live in the season's snapshot — episodes
		// deliberately carry no metadata_json. TMDB reports 0 for an unrated
		// episode, so null is the honest rendering of that.
		if ep, ok := s.SeasonEpisodeSnapshots(ctx, snaps, r.ShowID, r.SeasonNumber)[r.EpisodeNumber]; ok {
			if ep.VoteAverage > 0 {
				v := ep.VoteAverage
				entry.Episode.Rating = &v
			}
			entry.Episode.Marker = common.EpisodeMarker(ep)
		}

		progress, err := common.ShowProgress(ctx, s.DB, userID, r.ShowID, today)
		if err != nil {
			return nil, err
		}
		entry.Progress = UpNextProgress{
			Watched:    progress.Watched,
			AiredTotal: progress.AiredTotal,
			Total:      progress.Total,
		}

		entries = append(entries, entry)
	}
	return entries, nil
}

func parseBoolParam(name, value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%w: %s must be a boolean", httpx.ErrValidation, name)
	}
	return b, nil
}
