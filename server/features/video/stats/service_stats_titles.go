package stats

import (
	"context"
	"errors"
	"strconv"

	"mbyte.ro/sofalog/features/video/common"
	"mbyte.ro/sofalog/httpx"
)

// titleMemo resolves a movie or show into its StatsTitle once per request —
// a title referenced by several sections (the most rewatched movie is often
// also the top rated one) is read once. A row deleted between the bulk read
// and this point resolves to nil, which the callers treat as "one entry less"
// (a list) or "section null" (a singleton), the upcoming feed's own call —
// never a failed page.
type titleMemo struct {
	s      *Service
	movies map[int64]*StatsTitle
	shows  map[int64]*StatsTitle
}

func newTitleMemo(s *Service) *titleMemo {
	return &titleMemo{s: s, movies: map[int64]*StatsTitle{}, shows: map[int64]*StatsTitle{}}
}

// get resolves by kind: "movie" or "show".
func (m *titleMemo) get(ctx context.Context, kind string, id int64) (*StatsTitle, error) {
	if kind == "show" {
		return m.show(ctx, id)
	}
	return m.movie(ctx, id)
}

func (m *titleMemo) movie(ctx context.Context, id int64) (*StatsTitle, error) {
	if t, seen := m.movies[id]; seen {
		return t, nil
	}
	row, err := common.GetMovieByID(ctx, m.s.DB, id)
	if errors.Is(err, httpx.ErrNotFound) {
		m.movies[id] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t := &StatsTitle{Kind: "movie", ID: row.ID, Title: row.Title, Year: yearOfDate(row.ReleaseDate)}
	link := common.MovieLinker(row.ID, m.s.MovieFiles(row.ID))
	t.Poster, t.PosterFull = link.Poster(row.PosterPath)
	t.Backdrop, t.BackdropFull = link.Backdrop(row.BackdropPath)
	m.movies[id] = t
	return t, nil
}

func (m *titleMemo) show(ctx context.Context, id int64) (*StatsTitle, error) {
	if t, seen := m.shows[id]; seen {
		return t, nil
	}
	row, err := common.GetShowByID(ctx, m.s.DB, id)
	if errors.Is(err, httpx.ErrNotFound) {
		m.shows[id] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t := &StatsTitle{Kind: "show", ID: row.ID, Title: row.Title, Year: yearOfDate(row.FirstAirDate)}
	link := common.ShowLinker(row.ID, m.s.ShowFiles(row.ID))
	t.Poster, t.PosterFull = link.Poster(row.PosterPath)
	t.Backdrop, t.BackdropFull = link.Backdrop(row.BackdropPath)
	m.shows[id] = t
	return t, nil
}

// yearOfDate reads the year out of a YYYY-MM-DD date; nil for a missing,
// short or non-positive one. TMDB occasionally stores "" for an unknown date.
func yearOfDate(date *string) *int {
	if date == nil || len(*date) < 4 {
		return nil
	}
	y, err := strconv.Atoi((*date)[:4])
	if err != nil || y <= 0 {
		return nil
	}
	return &y
}
