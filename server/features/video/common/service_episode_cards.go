package common

import (
	"context"
	"encoding/json"

	"mbyte.ro/sofalog/provider"
)

// HistoryEpisode is the slice of one episode's snapshot the feed renders, kept
// so an episode repeated by a rewatch is projected once.
type HistoryEpisode struct {
	Poster     *string
	PosterFull *string
	Rating     *float64
	ShowID     int64
	ShowTitle  string
	SeasonID   *int64
	Season     int
	Number     int
	Title      string
}

// NewHistoryEpisode projects one episode for the feed. Its poster is the
// SEASON's, falling back to the show's — an episode still is 16:9 and would
// break a feed of 2:3 posters — and both resolve through the show's asset
// linker, so a tracked show's feed renders from disk with TMDB down.
//
// The community score lives in the season's snapshot rather than on the episode
// row (episodes deliberately carry no metadata_json), so it is read from there.
func (s *Core) NewHistoryEpisode(ctx context.Context, episodeID int64, snaps map[SeasonKey]map[int]provider.TMDBTVSeasonEpisode) (HistoryEpisode, error) {
	row, err := getHistoryEpisode(ctx, s.DB, episodeID)
	if err != nil {
		return HistoryEpisode{}, err
	}

	link := ShowLinker(row.ShowID, s.ShowFiles(row.ShowID))
	e := HistoryEpisode{
		ShowID:    row.ShowID,
		ShowTitle: row.ShowTitle,
		SeasonID:  row.SeasonID,
		Season:    row.SeasonNumber,
		Number:    row.EpisodeNumber,
	}
	if row.Title != nil {
		e.Title = *row.Title
	}
	switch {
	case row.SeasonPosterPath != nil:
		e.Poster, e.PosterFull = link.SeasonPoster(row.SeasonNumber, row.SeasonPosterPath)
	case row.ShowPosterPath != nil:
		e.Poster, e.PosterFull = link.Poster(row.ShowPosterPath)
	}
	// TMDB reports 0 for an unrated episode — null is the honest rendering.
	if ep, ok := s.SeasonEpisodeSnapshots(ctx, snaps, row.ShowID, row.SeasonNumber)[row.EpisodeNumber]; ok && ep.VoteAverage > 0 {
		v := ep.VoteAverage
		e.Rating = &v
	}
	return e, nil
}

// SeasonKey identifies one season's snapshot within a page's memo.
type SeasonKey struct {
	showID int64
	season int
}

// SeasonEpisodeSnapshots reads one season's episode entries out of its stored
// snapshot, keyed by episode number and memoised for the page — the history
// feed reads the community score, up-next also the episode_type marker. A
// missing or undecodable snapshot yields an empty map rather than an error: an
// entry without a rating or marker is a fine rendering, a failed page is not.
func (s *Core) SeasonEpisodeSnapshots(ctx context.Context, memo map[SeasonKey]map[int]provider.TMDBTVSeasonEpisode, showID int64, season int) map[int]provider.TMDBTVSeasonEpisode {
	key := SeasonKey{showID: showID, season: season}
	if cached, ok := memo[key]; ok {
		return cached
	}
	episodes := map[int]provider.TMDBTVSeasonEpisode{}
	memo[key] = episodes

	raw, err := seasonSnapshotJSON(ctx, s.DB, showID, season)
	if err != nil {
		return episodes
	}
	var snap SeasonSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return episodes
	}
	for _, e := range snap.Episodes {
		episodes[e.EpisodeNumber] = e
	}
	return episodes
}
