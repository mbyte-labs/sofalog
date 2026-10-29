package common

import (
	"context"
	"errors"

	"mbyte.ro/sofalog/httpx"
)

const (
	// ProviderBreakerTrips is how many consecutive provider outages the run
	// tolerates before it stops asking. Each untracked show costs a full
	// materialisation with its own timeouts, so a run that kept trying against
	// a dead TMDB would spend most of an hour failing.
	ProviderBreakerTrips = 3
)

const (
	SkipMovieNotFound = "movie_not_found"
)

const (
	SkipMovieUnavailable = "movie_unavailable"
)

const (
	SkipShowNotFound = "show_not_found"
)

const (
	SkipShowUnavailable = "show_unavailable"
)

const (
	SkipProviderUnavailable = "provider_unavailable"
)

const (
	SkipEpisodeNotFound = "episode_not_found"
)

// errProviderBreakerOpen marks a group the run refused to fetch because TMDB
// had already failed ProviderBreakerTrips times in a row.
var errProviderBreakerOpen = errors.New("provider circuit open")

// ProviderBreaker counts consecutive provider outages across the run.
type ProviderBreaker struct {
	consecutive int
}

func (b *ProviderBreaker) Open() bool { return b.consecutive >= ProviderBreakerTrips }

func (b *ProviderBreaker) Observe(err error) {
	switch {
	case err == nil:
		b.consecutive = 0
	case errors.Is(err, httpx.ErrProviderUnavailable):
		b.consecutive++
	}
}

// GroupSkipReason maps a resolution failure to the skip reason for the whole
// group, or reports that the error is not one the run can absorb.
func GroupSkipReason(err error, notFound, unavailable string) (string, bool) {
	switch {
	case errors.Is(err, errProviderBreakerOpen):
		return SkipProviderUnavailable, true
	case errors.Is(err, httpx.ErrNotFound):
		return notFound, true
	case errors.Is(err, httpx.ErrProviderUnavailable):
		return unavailable, true
	}
	return "", false
}

// ResolveRestoreMovie finds the movie's catalog row, materialising it when
// the file names one sofalog has never seen. Reports whether it did.
func (s *Core) ResolveRestoreMovie(ctx context.Context, tmdbID int64, breaker *ProviderBreaker) (MovieRow, bool, error) {
	row, err := GetMovieByTMDBID(ctx, s.DB, tmdbID)
	if err == nil {
		return row, false, nil
	}
	if !errors.Is(err, httpx.ErrNotFound) {
		return MovieRow{}, false, err
	}
	if breaker.Open() {
		return MovieRow{}, false, errProviderBreakerOpen
	}
	row, err = s.RefreshMovie(ctx, tmdbID)
	breaker.Observe(err)
	return row, err == nil, err
}

// ResolveRestoreShow is ResolveRestoreMovie for a show: the whole show, every
// season and episode, through the one materialisation gate.
func (s *Core) ResolveRestoreShow(ctx context.Context, tmdbID int64, breaker *ProviderBreaker) (ShowRow, bool, error) {
	row, err := GetShowByTMDBID(ctx, s.DB, tmdbID)
	if err == nil {
		return row, false, nil
	}
	if !errors.Is(err, httpx.ErrNotFound) {
		return ShowRow{}, false, err
	}
	if breaker.Open() {
		return ShowRow{}, false, errProviderBreakerOpen
	}
	row, err = s.ResolveTrackedShow(ctx, tmdbID)
	breaker.Observe(err)
	return row, err == nil, err
}
