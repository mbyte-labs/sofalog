package stats

import (
	"context"
	"encoding/json"
	"sort"
)

// titleKey identifies a movie or a show: the pair, never the id alone, since
// movie 5 and show 5 both exist (D-013).
type titleKey struct {
	Kind string
	ID   int64
}

// titleAgg is one distinct watched title: a movie, or a show every episode
// play rolls up to. Plays and the last play instant feed the rewatch and
// tie-break rules; Year and Genres come from the catalog row the bulk read
// carried along.
type titleAgg struct {
	Key    titleKey
	Plays  int
	LastAt string
	Year   *int
	Genres *string
}

// aggregateTitles folds the chronological play rows into distinct titles, in
// first-seen order. Rows arrive oldest first, so the last assignment of LastAt
// is the most recent play.
func aggregateTitles(plays []playRow) []titleAgg {
	index := map[titleKey]int{}
	var out []titleAgg
	for _, r := range plays {
		var key titleKey
		var year *int
		var genres *string
		if r.TargetType == "episode" {
			if r.ShowID == nil {
				continue
			}
			key = titleKey{"show", *r.ShowID}
			year, genres = yearOfDate(r.ShowFirstAir), r.ShowGenres
		} else {
			key = titleKey{"movie", r.TargetID}
			year, genres = yearOfDate(r.MovieRelease), r.MovieGenres
		}
		i, ok := index[key]
		if !ok {
			index[key] = len(out)
			out = append(out, titleAgg{Key: key, Year: year, Genres: genres})
			i = len(out) - 1
		}
		out[i].Plays++
		out[i].LastAt = r.WatchedAt
	}
	return out
}

// lessKey is the deterministic tail of every ordering: kind, then id.
func lessKey(a, b titleKey) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ID < b.ID
}

// showRuntimeFallbacks reads TMDB's regular episode runtime out of each show
// body: the first entry of episode_run_time, when positive. A show without one
// contributes 0 minutes for its runtime-less episodes.
func showRuntimeFallbacks(docs []showDoc) map[int64]int {
	out := make(map[int64]int, len(docs))
	for _, d := range docs {
		var body struct {
			EpisodeRunTime []int `json:"episode_run_time"`
		}
		if err := json.Unmarshal([]byte(d.MetadataJSON), &body); err != nil {
			continue
		}
		if len(body.EpisodeRunTime) > 0 && body.EpisodeRunTime[0] > 0 {
			out[d.ShowID] = body.EpisodeRunTime[0]
		}
	}
	return out
}

// minutesOf is one play's runtime: the movie's, or the episode's falling back
// to its show's regular runtime, else 0 — so the hours can only go up as
// snapshots fill in, never read as a wrong number.
func minutesOf(r playRow, fallbacks map[int64]int) int {
	if r.TargetType == "movie" {
		if r.MovieRuntime != nil {
			return *r.MovieRuntime
		}
		return 0
	}
	if r.EpisodeRuntime != nil {
		return *r.EpisodeRuntime
	}
	if r.ShowID != nil {
		return fallbacks[*r.ShowID]
	}
	return 0
}

// headline is the 2×2 grid plus the minutes behind the TV-vs-movies split.
// Movies and episodes are DISTINCT targets, so a rewatch counts once; a play
// without a known runtime counts 0 minutes.
func (c *calc) headline(plays []playRow, fallbacks map[int64]int) StatsHeadline {
	h := StatsHeadline{Plays: len(plays)}
	movies := map[int64]bool{}
	episodes := map[int64]bool{}
	for _, r := range plays {
		if r.TargetType == "movie" {
			movies[r.TargetID] = true
			h.MovieMinutes += minutesOf(r, fallbacks)
		} else {
			episodes[r.TargetID] = true
			h.EpisodeMinutes += minutesOf(r, fallbacks)
		}
	}
	h.Movies = len(movies)
	h.Episodes = len(episodes)
	h.Hours = (h.MovieMinutes + h.EpisodeMinutes) / 60
	return h
}

// mostWatchedShow rolls every episode play up to its show and picks the one
// with the most minutes, then the most plays, then the most recent play, then
// the lowest id — so with no runtime known anywhere, plays decide. Null
// without an episode play, or when the winning show's row is gone.
func (c *calc) mostWatchedShow(ctx context.Context, plays []playRow, fallbacks map[int64]int) (*StatsMostWatchedShow, error) {
	type agg struct {
		id      int64
		minutes int
		plays   int
		last    string
	}
	index := map[int64]int{}
	var shows []agg
	for _, r := range plays {
		if r.TargetType != "episode" || r.ShowID == nil {
			continue
		}
		i, ok := index[*r.ShowID]
		if !ok {
			index[*r.ShowID] = len(shows)
			shows = append(shows, agg{id: *r.ShowID})
			i = len(shows) - 1
		}
		shows[i].minutes += minutesOf(r, fallbacks)
		shows[i].plays++
		shows[i].last = r.WatchedAt
	}
	if len(shows) == 0 {
		return nil, nil
	}
	sort.Slice(shows, func(i, j int) bool {
		a, b := shows[i], shows[j]
		if a.minutes != b.minutes {
			return a.minutes > b.minutes
		}
		if a.plays != b.plays {
			return a.plays > b.plays
		}
		if a.last != b.last {
			return a.last > b.last
		}
		return a.id < b.id
	})
	for _, sh := range shows {
		title, err := c.titles.show(ctx, sh.id)
		if err != nil {
			return nil, err
		}
		if title == nil {
			continue
		}
		return &StatsMostWatchedShow{Show: *title, Minutes: sh.minutes, Plays: sh.plays, LastPlayedAt: sh.last}, nil
	}
	return nil, nil
}

// unratedMovies lists the watched movies with no movie rating IN THE PERIOD
// (in a year, "rated" means rated that year — a movie watched in 2026 but rated
// in 2025 is listed as unrated in the 2026 view), most recent play first, then
// lowest id. Total counts all of them; Recent carries the first five.
func (c *calc) unratedMovies(ctx context.Context, titles []titleAgg, ratings []ratingRow) (*StatsUnratedMovies, error) {
	rated := map[int64]bool{}
	for _, r := range ratings {
		if r.TargetType == "movie" {
			rated[r.TargetID] = true
		}
	}
	var movies []titleAgg
	for _, t := range titles {
		if t.Key.Kind == "movie" && !rated[t.Key.ID] {
			movies = append(movies, t)
		}
	}
	if len(movies) == 0 {
		return nil, nil
	}
	sort.Slice(movies, func(i, j int) bool {
		if movies[i].LastAt != movies[j].LastAt {
			return movies[i].LastAt > movies[j].LastAt
		}
		return movies[i].Key.ID < movies[j].Key.ID
	})
	out := &StatsUnratedMovies{Total: len(movies), Recent: make([]StatsUnratedMovie, 0, unratedLimit)}
	for _, m := range movies {
		if len(out.Recent) == unratedLimit {
			break
		}
		title, err := c.titles.movie(ctx, m.Key.ID)
		if err != nil {
			return nil, err
		}
		if title == nil {
			continue
		}
		out.Recent = append(out.Recent, StatsUnratedMovie{Movie: *title, LastPlayedAt: m.LastAt})
	}
	return out, nil
}

// decades buckets the distinct watched titles by release decade — each movie
// once, each show with a played episode once. Buckets are contiguous from the
// oldest to the newest decade so the chart has no gaps; the favourite is the
// fullest decade, a tie going to the more recent; oldest and newest are the
// titles with the extreme years, a tie going to more plays, then the more
// recent play, then kind and id. Null when no title has a year.
func (c *calc) decades(ctx context.Context, titles []titleAgg) (*StatsDecades, error) {
	var dated []titleAgg
	for _, t := range titles {
		if t.Year != nil {
			dated = append(dated, t)
		}
	}
	if len(dated) == 0 {
		return nil, nil
	}
	counts := map[int]int{}
	minDec, maxDec := 0, 0
	for i, t := range dated {
		dec := *t.Year / 10 * 10
		counts[dec]++
		if i == 0 || dec < minDec {
			minDec = dec
		}
		if i == 0 || dec > maxDec {
			maxDec = dec
		}
	}
	out := &StatsDecades{Covered: len(dated), Total: len(titles), Buckets: make([]StatsDecade, 0, (maxDec-minDec)/10+1)}
	for dec := minDec; dec <= maxDec; dec += 10 {
		out.Buckets = append(out.Buckets, StatsDecade{Decade: dec, Titles: counts[dec]})
		if counts[dec] >= counts[out.Favorite] {
			out.Favorite = dec
		}
	}

	// The era tie-break: more plays, then the more recent play, then key.
	prefer := func(a, b titleAgg) bool {
		if a.Plays != b.Plays {
			return a.Plays > b.Plays
		}
		if a.LastAt != b.LastAt {
			return a.LastAt > b.LastAt
		}
		return lessKey(a.Key, b.Key)
	}
	oldest, newest := dated[0], dated[0]
	for _, t := range dated[1:] {
		switch {
		case *t.Year < *oldest.Year, *t.Year == *oldest.Year && prefer(t, oldest):
			oldest = t
		}
		switch {
		case *t.Year > *newest.Year, *t.Year == *newest.Year && prefer(t, newest):
			newest = t
		}
	}
	era := func(t titleAgg) (*StatsEraTitle, error) {
		title, err := c.titles.get(ctx, t.Key.Kind, t.Key.ID)
		if err != nil || title == nil {
			return nil, err
		}
		return &StatsEraTitle{Title: *title, Year: *t.Year}, nil
	}
	var err error
	if out.Oldest, err = era(oldest); err != nil {
		return nil, err
	}
	if len(dated) > 1 {
		if out.Newest, err = era(newest); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// mostRewatched lists the titles played more than once — a show's count being
// the sum of its episode plays — most plays first, then the most recent play,
// then kind and id, at most five. Empty, never null, when nothing was rewatched.
func (c *calc) mostRewatched(ctx context.Context, titles []titleAgg) ([]StatsRewatched, error) {
	var multi []titleAgg
	for _, t := range titles {
		if t.Plays > 1 {
			multi = append(multi, t)
		}
	}
	sort.Slice(multi, func(i, j int) bool {
		a, b := multi[i], multi[j]
		if a.Plays != b.Plays {
			return a.Plays > b.Plays
		}
		if a.LastAt != b.LastAt {
			return a.LastAt > b.LastAt
		}
		return lessKey(a.Key, b.Key)
	})
	out := make([]StatsRewatched, 0, rewatchedLimit)
	for _, t := range multi {
		if len(out) == rewatchedLimit {
			break
		}
		title, err := c.titles.get(ctx, t.Key.Kind, t.Key.ID)
		if err != nil {
			return nil, err
		}
		if title == nil {
			continue
		}
		out = append(out, StatsRewatched{Title: *title, Plays: t.Plays, LastPlayedAt: t.LastAt})
	}
	return out, nil
}

// genres counts, over the distinct watched titles of the period (movies and
// shows alike), how many titles list each genre name — each title once per
// name, even if its stored list repeats one. Most titles first, then name,
// at most five. Covered is how many titles had a readable, non-empty list.
// Null when the period holds no title at all; an empty entry list with titles
// present is a legal answer (nothing had genres yet).
func genres(titles []titleAgg) *StatsGenres {
	if len(titles) == 0 {
		return nil
	}
	counts := map[string]int{}
	covered := 0
	for _, t := range titles {
		if t.Genres == nil {
			continue
		}
		var names []string
		if err := json.Unmarshal([]byte(*t.Genres), &names); err != nil || len(names) == 0 {
			continue
		}
		seen := map[string]bool{}
		for _, n := range names {
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			counts[n]++
		}
		if len(seen) > 0 {
			covered++
		}
	}
	entries := make([]StatsGenre, 0, len(counts))
	for n, k := range counts {
		entries = append(entries, StatsGenre{Name: n, Titles: k})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Titles != entries[j].Titles {
			return entries[i].Titles > entries[j].Titles
		}
		return entries[i].Name < entries[j].Name
	})
	if len(entries) > genresLimit {
		entries = entries[:genresLimit]
	}
	return &StatsGenres{Entries: entries, Covered: covered, Total: len(titles)}
}
