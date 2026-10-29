package upcoming

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/provider"
)

// UpcomingEntry is one dated row of the upcoming calendar: an episode of a
// followed or watchlisted show that has not aired (or aired today and is still
// unwatched), or an unreleased movie on the watchlist.
//
// Show + Episode and Movie form a discriminated union keyed by Kind: for an
// episode both Show and Episode are non-nil and Movie is nil; for a movie the
// reverse. Nullable fields are pointers without omitempty — the SPA
// distinguishes null from absent.
//
// The list is flat and ordered by Date, then title; the SPA folds adjacent
// entries into day sections and collapses a same-show run (a whole season
// released in one day) into one row. The server never groups — the history
// feed's convention.
type UpcomingEntry struct {
	// The day the entry lands on: the episode's air date or the movie's
	// release date, YYYY-MM-DD, as TMDB states it. A day, not an instant.
	Date string `json:"date"`
	Kind string `json:"kind"` // episode | movie
	// Relative asset paths, or null together: the season's poster falling
	// back to the show's for an episode, the movie's for a movie — at the
	// grid size (w185) and the full size (w500), offered together through
	// srcset.
	Poster     *string          `json:"poster"`
	PosterFull *string          `json:"poster_full"`
	Show       *UpcomingShow    `json:"show"`    // non-nil iff kind == "episode"
	Episode    *UpcomingEpisode `json:"episode"` // non-nil iff kind == "episode"
	Movie      *UpcomingMovie   `json:"movie"`   // non-nil iff kind == "movie"
}

type UpcomingShow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// UpcomingEpisode carries no rating and no aired flag: an upcoming episode is
// by construction unaired-or-today and has no community score yet.
type UpcomingEpisode struct {
	ID int64 `json:"id"`
	// The local seasons.id — the handle the episode details page is addressed
	// by (an episode has no id-addressed endpoint), exactly as UpNextEpisode
	// carries it. Null only if the season row is gone.
	SeasonID *int64  `json:"season_id"`
	Season   int     `json:"season"`
	Number   int     `json:"number"`
	Title    string  `json:"title"`
	Marker   *string `json:"marker"` // season_premiere | mid_season_finale | season_finale | null
}

type UpcomingMovie struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// UpcomingParams carries the query parameters as received; Upcoming defaults
// and validates them so the handler stays free of domain branching.
type UpcomingParams struct {
	Type string // "" | all | episode | movie
	// Days is the horizon: the window is [From, From+Days), 1..366, default 90.
	Days string
	// From is the first day of the window, YYYY-MM-DD; "" = the user's today,
	// in their zone (D-038). The SPA sends the same day, computed in the same
	// zone; the parameter stays so a client can page the calendar forward.
	From string
}

const (
	upcomingDefaultDays = 90
	upcomingMaxDays     = 366
)

// Upcoming lists the dated things this user is waiting for, in calendar order.
// It is a pure local read — never a provider call site — so it cannot 503.
func (s *Service) Upcoming(ctx context.Context, userID int64, p UpcomingParams) ([]UpcomingEntry, error) {
	kind := p.Type
	if kind == "" {
		kind = "all"
	}
	switch kind {
	case "all", "episode", "movie":
	default:
		return nil, fmt.Errorf("%w: type must be one of all, episode, movie", httpx.ErrValidation)
	}

	days := upcomingDefaultDays
	if p.Days != "" {
		n, err := strconv.Atoi(p.Days)
		if err != nil || n < 1 || n > upcomingMaxDays {
			return nil, fmt.Errorf("%w: days must be an integer between 1 and %d", httpx.ErrValidation, upcomingMaxDays)
		}
		days = n
	}

	day := p.From
	if day == "" {
		clk, err := s.ClockFor(ctx, userID)
		if err != nil {
			return nil, err
		}
		day = clk.Today
	}
	from, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return nil, fmt.Errorf("%w: from must be a YYYY-MM-DD date", httpx.ErrValidation)
	}
	// Both bounds are re-formatted from the parsed instant, so the strings
	// compared against the YYYY-MM-DD columns are always fixed-width dates.
	fromDay := from.Format(time.DateOnly)
	untilDay := from.AddDate(0, 0, days).Format(time.DateOnly)

	rows, err := listUpcoming(ctx, s.DB, upcomingFilter{
		UserID: userID,
		Type:   kind,
		From:   fromDay,
		Until:  untilDay,
	})
	if err != nil {
		return nil, err
	}

	// One snapshot parse per (show, season), not per entry — the history feed's
	// memo, filled by NewHistoryEpisode and read again for the marker.
	snaps := map[common.SeasonKey]map[int]provider.TMDBTVSeasonEpisode{}
	// The empty literal keeps the contract a bare array — never encode null.
	entries := make([]UpcomingEntry, 0, len(rows))
	for _, r := range rows {
		entry := UpcomingEntry{Date: r.Date, Kind: r.Kind}
		var err error
		switch r.Kind {
		case "episode":
			err = s.upcomingEpisode(ctx, r.ItemID, snaps, &entry)
		default:
			err = s.upcomingMovie(ctx, r.ItemID, &entry)
		}
		// The query saw the catalog row one statement ago; a row deleted in
		// between is one entry less, not a failed calendar — the watchlist and
		// history feeds make the same call.
		if errors.Is(err, httpx.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// upcomingEpisode fills the episode arm through the history feed's projection,
// so poster choice (season, falling back to show) and the season_id handle have
// one definition, then reads the marker out of the same memoised snapshot.
func (s *Service) upcomingEpisode(ctx context.Context, episodeID int64, snaps map[common.SeasonKey]map[int]provider.TMDBTVSeasonEpisode, entry *UpcomingEntry) error {
	ep, err := s.NewHistoryEpisode(ctx, episodeID, snaps)
	if err != nil {
		return err
	}
	entry.Poster, entry.PosterFull = ep.Poster, ep.PosterFull
	entry.Show = &UpcomingShow{ID: ep.ShowID, Name: ep.ShowTitle}
	entry.Episode = &UpcomingEpisode{
		ID:       episodeID,
		SeasonID: ep.SeasonID,
		Season:   ep.Season,
		Number:   ep.Number,
		Title:    ep.Title,
	}
	if snap, ok := s.SeasonEpisodeSnapshots(ctx, snaps, ep.ShowID, ep.Season)[ep.Number]; ok {
		entry.Episode.Marker = common.EpisodeMarker(snap)
	}
	return nil
}

// upcomingMovie fills the movie arm. The poster goes through the movie's asset
// linker like every other card, so a tracked movie renders from disk with TMDB
// down; no snapshot is parsed, since a card needs a title and a poster only.
func (s *Service) upcomingMovie(ctx context.Context, movieID int64, entry *UpcomingEntry) error {
	row, err := common.GetMovieByID(ctx, s.DB, movieID)
	if err != nil {
		return err
	}
	entry.Movie = &UpcomingMovie{ID: row.ID, Title: row.Title}
	entry.Poster, entry.PosterFull = common.MovieLinker(row.ID, s.MovieFiles(row.ID)).Poster(row.PosterPath)
	return nil
}
