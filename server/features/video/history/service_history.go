package history

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// HistoryEntry is one row of the "History by date" feed: exactly one watch as
// recorded in user_watch_history. Rewatches are real rows (D-009), so the same
// target legitimately appears more than once — ID, the watch-row id, is the
// only stable identity a client may key a list on.
//
// Movie and Episode form a discriminated union keyed by TargetType: exactly one
// is non-nil. The pair (TargetType, that object's ID) identifies the target;
// there is deliberately no bare target_id in the payload, because an id alone
// identifies nothing — movie 5 and episode 5 both exist (D-013).
//
// Nullable fields are pointers without omitempty — the SPA distinguishes null
// from absent. Entries are ordered by WatchedAt then ID (both following the
// requested direction), so entries sharing a month are contiguous and the SPA
// can group them with a single pass over adjacent entries. WatchedAt is an
// instant, so two watches on one day order by time of day; the ID tie-break
// still matters, because a bulk mark writes one timestamp to many rows.
type HistoryEntry struct {
	ID int64 `json:"id"`
	// The instant the watch happened, RFC3339 UTC — not the time the row was
	// written, which a backdated entry sets differently. Clients render it in
	// the viewer's local time; the server never emits an offset.
	WatchedAt string `json:"watched_at"`
	// PROVENANCE ONLY (D-010): watch | checkin | scrobble. It records how the
	// watch was captured; no query and no client may branch on this value.
	WatchType  string `json:"watch_type"`
	TargetType string `json:"target_type"` // movie | episode
	// Relative asset paths, or null together. Resolved server-side for both
	// kinds — the movie poster, or for an episode the season poster falling
	// back to the show poster — at the grid size (w185) and the full size
	// (w500), offered together through srcset. Hoisted out of the union: one
	// image slot per row.
	Poster     *string `json:"poster"`
	PosterFull *string `json:"poster_full"`
	// THIS user's rating of the watched target, 1–10, or null when unrated.
	// Named user_rating, not rating, because UpNextEpisode.Rating is the TMDB
	// community score — a float, a different thing. A rating belongs to the
	// target, not to the watch: re-rating overwrites and there is no rating
	// history, so two entries for the same target always carry the same value.
	UserRating *int `json:"user_rating"`
	// TMDB community score (0–10) of the watched target from the stored
	// metadata snapshot, or null when the snapshot has none. A float and a
	// provider value — the counterpart of UpNextEpisode.Rating, and a different
	// thing from UserRating above.
	Rating  *float64        `json:"rating"`
	Movie   *HistoryMovie   `json:"movie"`   // non-nil iff target_type == "movie"
	Episode *HistoryEpisode `json:"episode"` // non-nil iff target_type == "episode"
}

type HistoryMovie struct {
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	ReleaseDate *string `json:"release_date"` // YYYY-MM-DD, or null when undated
}

type HistoryEpisode struct {
	ID   int64       `json:"id"`
	Show HistoryShow `json:"show"`
	// SeasonID is the local seasons.id, and the only handle in this arm that
	// addresses anything: an episode has no id-addressed endpoint, so the
	// episode details page is reached by its season plus Number. Null only if
	// the season row has gone, which the catalog write order makes unreachable
	// in practice — see historyEpisodeRow.SeasonID.
	SeasonID *int64 `json:"season_id"`
	Season   int    `json:"season"`
	Number   int    `json:"number"`
	Title    string `json:"title"`
}

// HistoryShow duplicates UpNextShow field for field on purpose: the two
// payloads are separate /api/v1 contracts and must version independently.
type HistoryShow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// HistoryParams carries the query parameters as received; History defaults and
// validates them so the handler stays free of domain branching.
type HistoryParams struct {
	Order string // "" | asc | desc
	Type  string // "" | all | movie | episode
	// TargetType and TargetID restrict the feed to every watch of one item —
	// both or neither, because the id alone identifies nothing (D-013).
	TargetType string // "" | movie | episode
	TargetID   string // positive integer, required with TargetType
	// SeasonID and ShowID restrict the feed to one season's, or one whole
	// show's, episode watches. Scopes, not targets: neither is ever itself
	// watched, so neither is a TargetType value. At most one narrowing may be
	// set across these two and the pair above.
	SeasonID string // positive integer; "" = no season scope
	ShowID   string // positive integer; "" = no show scope
	// Limit and Offset slice the feed after ordering. The response stays a
	// bare array; a client asking for limit N knows more may exist exactly
	// when it received N entries back.
	Limit  string // positive integer; "" = unlimited
	Offset string // non-negative integer; "" = 0
}

func (s *Service) History(ctx context.Context, userID int64, p HistoryParams) ([]HistoryEntry, error) {
	order := p.Order
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return nil, fmt.Errorf("%w: order must be asc or desc", httpx.ErrValidation)
	}

	targetType := p.Type
	if targetType == "" {
		targetType = "all"
	}
	switch targetType {
	case "all", "movie", "episode":
	default:
		return nil, fmt.Errorf("%w: type must be one of all, movie, episode", httpx.ErrValidation)
	}

	if (p.TargetType == "") != (p.TargetID == "") {
		return nil, fmt.Errorf("%w: target_type and target_id must be provided together", httpx.ErrValidation)
	}

	// Three narrowings, at most one at a time. Counted rather than compared
	// pairwise so a fourth cannot quietly slip through: letting one scope win
	// silently over another is a bug the caller would never see.
	narrowings := 0
	for _, set := range []bool{p.TargetType != "", p.SeasonID != "", p.ShowID != ""} {
		if set {
			narrowings++
		}
	}
	if narrowings > 1 {
		return nil, fmt.Errorf(
			"%w: target_type, season_id and show_id are mutually exclusive", httpx.ErrValidation)
	}

	var filterID int64
	if p.TargetType != "" {
		switch p.TargetType {
		case "movie", "episode":
		default:
			return nil, fmt.Errorf("%w: target_type must be movie or episode", httpx.ErrValidation)
		}
		id, err := strconv.ParseInt(p.TargetID, 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("%w: target_id must be a positive integer", httpx.ErrValidation)
		}
		filterID = id
	}

	var seasonShowTMDBID int64
	var seasonNumber *int
	if p.SeasonID != "" {
		id, err := strconv.ParseInt(p.SeasonID, 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("%w: season_id must be a positive integer", httpx.ErrValidation)
		}
		// The season row is what turns an id into the (show tmdb_id, number)
		// pair episodes are actually keyed by — and it gives an unknown id a
		// 404 for free.
		season, err := common.GetSeasonByID(ctx, s.DB, id)
		if err != nil {
			return nil, err
		}
		seasonShowTMDBID = season.ShowTMDBID
		seasonNumber = &season.SeasonNumber
	}

	var showID int64
	if p.ShowID != "" {
		id, err := strconv.ParseInt(p.ShowID, 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("%w: show_id must be a positive integer", httpx.ErrValidation)
		}
		// Episodes carry show_id directly, so this read exists only to turn an
		// unknown id into a 404 rather than a silently empty feed.
		if _, err := common.GetShowByID(ctx, s.DB, id); err != nil {
			return nil, err
		}
		showID = id
	}

	limit := -1
	if p.Limit != "" {
		n, err := strconv.Atoi(p.Limit)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%w: limit must be a positive integer", httpx.ErrValidation)
		}
		limit = n
	}
	offset := 0
	if p.Offset != "" {
		n, err := strconv.Atoi(p.Offset)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%w: offset must be a non-negative integer", httpx.ErrValidation)
		}
		offset = n
	}

	// Ordering and paging happen in the store; the same query serves the whole
	// feed, one kind of target, every watch of one item, and the season- and
	// show-scoped feeds.
	typeFilter := targetType
	if typeFilter == "all" {
		typeFilter = ""
	}
	watches, err := listWatchPage(ctx, s.DB, watchFilter{
		UserID:           userID,
		Type:             typeFilter,
		TargetType:       p.TargetType,
		TargetID:         filterID,
		SeasonShowTMDBID: seasonShowTMDBID,
		SeasonNumber:     seasonNumber,
		ShowID:           showID,
		Desc:             order == "desc",
		Limit:            limit,
		Offset:           offset,
	})
	if err != nil {
		return nil, err
	}

	// Each distinct target is read and projected ONCE per page: a rewatch is a
	// second row for the same target (D-009), and the snapshot plus its on-disk
	// asset listing are the expensive part. The empty literal keeps the contract
	// a bare array — never encode null.
	movies := map[int64]historyMovie{}
	episodes := map[int64]common.HistoryEpisode{}
	// One parse per season, not per episode: a page of a single season's
	// rewatches would otherwise decode the same document twenty times.
	snaps := map[common.SeasonKey]map[int]provider.TMDBTVSeasonEpisode{}
	entries := make([]HistoryEntry, 0, len(watches))
	for _, w := range watches {
		entry := HistoryEntry{
			ID:         w.ID,
			WatchedAt:  w.WatchedAt,
			WatchType:  w.WatchType,
			TargetType: w.TargetType,
			UserRating: w.UserRating,
		}

		if w.TargetType == "episode" {
			e, ok := episodes[w.TargetID]
			if !ok {
				loaded, err := s.NewHistoryEpisode(ctx, w.TargetID, snaps)
				switch {
				case errors.Is(err, httpx.ErrNotFound):
					// Untracked between the two reads — separate statements on
					// the read pool, and target_id has no foreign key.
					continue
				case err != nil:
					return nil, err
				}
				e = loaded
				episodes[w.TargetID] = e
			}
			entry.Poster, entry.PosterFull = e.Poster, e.PosterFull
			entry.Rating = e.Rating
			entry.Episode = &HistoryEpisode{
				ID:       w.TargetID,
				Show:     HistoryShow{ID: e.ShowID, Name: e.ShowTitle},
				SeasonID: e.SeasonID,
				Season:   e.Season,
				Number:   e.Number,
				Title:    e.Title,
			}
			// Movie stays nil and so serializes as an explicit null: the union
			// key is part of the published /api/v1 shape, empty arm and all.
			entries = append(entries, entry)
			continue
		}

		m, ok := movies[w.TargetID]
		if !ok {
			row, err := common.GetMovieByID(ctx, s.DB, w.TargetID)
			switch {
			case errors.Is(err, httpx.ErrNotFound):
				continue
			case err != nil:
				return nil, err
			}
			m = newHistoryMovie(row, s.MovieFiles(row.ID), s.WatchRegion)
			movies[w.TargetID] = m
		}
		entry.Poster, entry.PosterFull = m.poster, m.posterFull
		entry.Rating = m.rating
		entry.Movie = &HistoryMovie{
			ID:          w.TargetID,
			Title:       m.title,
			ReleaseDate: m.releaseDate,
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// historyMovie is the slice of one movie's snapshot the feed renders, kept so a
// movie repeated by a rewatch is projected once.
type historyMovie struct {
	poster      *string
	posterFull  *string
	rating      *float64
	title       string
	releaseDate *string
}

// newHistoryMovie projects a catalog row through the same details projection the
// movie page uses, so poster choice and community rating have a single source
// and the feed renders identically with TMDB down.
func newHistoryMovie(row common.MovieRow, files map[string]bool, region string) historyMovie {
	details := common.MovieDetailsFromRow(row, files, region)
	return historyMovie{
		poster:      details.Poster,
		posterFull:  details.PosterFull,
		rating:      details.Rating,
		title:       row.Title,
		releaseDate: row.ReleaseDate,
	}
}
