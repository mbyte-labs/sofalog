package stats

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"

	"mbyte.ro/sofalog/httpx"
	"mbyte.ro/sofalog/tz"
)

// Stats is the whole stats page for one user and one period (D-039). Every
// section is always present; a section the page would hide is null (or an
// empty list where the spec lists items), so the SPA decides visibility from
// the data and never from a flag. Nullable fields are pointers without
// omitempty — the SPA distinguishes null from absent. Lists are never null.
type Stats struct {
	// The APPLIED period: "all", or a four-digit year in the user's zone. A
	// request without a period gets the current year when it holds at least
	// one play, else "all"; what was applied is always echoed.
	Period string `json:"period"`
	// Every year the filter offers, newest first: from the earlier of the
	// first play and the first rating through the current year. Empty when
	// there is no play and no rating.
	Years []int `json:"years"`
	// The user's calendar day (D-038): what "watched … ago", the heatmap's
	// last cell and the streaks are anchored on.
	Today string `json:"today"`

	Headline        StatsHeadline         `json:"headline"`
	MostWatchedShow *StatsMostWatchedShow `json:"most_watched_show"` // null: no episode play in the period
	Ratings         *StatsRatings         `json:"ratings"`           // null: no rating in the period
	TopRated        []StatsTopRated       `json:"top_rated"`         // at most 10
	RatingTrend     *StatsRatingTrend     `json:"rating_trend"`      // null: no movie or show rating in the period
	Crowd           *StatsCrowd           `json:"crowd"`             // null: fewer than 5 rated titles comparable with TMDB
	UnratedMovies   *StatsUnratedMovies   `json:"unrated_movies"`    // null: every watched movie is rated
	Activity        StatsActivity         `json:"activity"`
	Decades         *StatsDecades         `json:"decades"`        // null: no watched title has a release year
	MostRewatched   []StatsRewatched      `json:"most_rewatched"` // at most 5
	Completion      *StatsCompletion      `json:"completion"`     // null: a year, or no followed show
	Genres          *StatsGenres          `json:"genres"`         // null: no watched title in the period
}

// StatsTitle is the one way a section refers to a movie or a show. An episode
// is never a title: it refers to its show, the way its history card links.
// Every stats title is tracked (it was watched or rated), so the artwork is the
// item's own through the asset linker — never a provider URL.
type StatsTitle struct {
	Kind  string `json:"kind"` // movie | show
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Year  *int   `json:"year"` // release / first-air year; null when undated
	// Relative URLs at both sizes (posters w185 / w500, backdrops w300 /
	// w1280) through the asset linker; each pair is null together when the
	// item has no such image. The SPA offers both through srcset.
	Poster       *string `json:"poster"`
	PosterFull   *string `json:"poster_full"`
	Backdrop     *string `json:"backdrop"`
	BackdropFull *string `json:"backdrop_full"`
}

type StatsHeadline struct {
	Hours    int `json:"hours"`    // (movie minutes + episode minutes) / 60, floored
	Plays    int `json:"plays"`    // watch-log rows in the period
	Movies   int `json:"movies"`   // distinct movies with at least one play
	Episodes int `json:"episodes"` // distinct episodes with at least one play
	// The two halves of the TV-vs-movies split, in minutes; both 0 when no
	// play has a known runtime. A play whose runtime is unknown counts 0.
	MovieMinutes   int `json:"movie_minutes"`
	EpisodeMinutes int `json:"episode_minutes"`
}

type StatsMostWatchedShow struct {
	Show         StatsTitle `json:"show"`
	Minutes      int        `json:"minutes"`
	Plays        int        `json:"plays"`          // episode plays
	LastPlayedAt string     `json:"last_played_at"` // RFC3339
}

type StatsRatings struct {
	Distribution    [10]int  `json:"distribution"` // index 0 = rating 1
	Count           int      `json:"count"`
	Average         float64  `json:"average"`
	MoviesAverage   *float64 `json:"movies_average"`   // null without a movie rating
	ShowsAverage    *float64 `json:"shows_average"`    // null without a show rating
	EpisodesAverage *float64 `json:"episodes_average"` // null without an episode rating
}

type StatsTopRated struct {
	Title   StatsTitle `json:"title"`
	Rating  int        `json:"rating"`
	RatedAt string     `json:"rated_at"`
}

type StatsRatingTrend struct {
	Months  []StatsRatingMonth `json:"months"`  // exactly 12, oldest first
	Recent  StatsRatingSample  `json:"recent"`  // the 90 days before the reference
	Earlier StatsRatingSample  `json:"earlier"` // everything before that window, in the period
	// Recent minus earlier, rounded to one decimal; null unless both sides
	// hold at least 3 ratings. Verdict is derived from the rounded value.
	Delta   *float64 `json:"delta"`
	Verdict *string  `json:"verdict"` // harsher | kinder | steady; null with Delta
	// The window's end: now, or the end of a past year. RFC3339.
	ReferenceAt string `json:"reference_at"`
}

type StatsRatingMonth struct {
	Month   string   `json:"month"` // YYYY-MM in the user's zone
	Count   int      `json:"count"`
	Average *float64 `json:"average"` // null when count is 0
}

type StatsRatingSample struct {
	Count   int      `json:"count"`
	Average *float64 `json:"average"` // null when count is 0
}

type StatsCrowd struct {
	UserAverage  float64           `json:"user_average"`
	CrowdAverage float64           `json:"crowd_average"`
	Delta        float64           `json:"delta"`   // user minus crowd
	Verdict      string            `json:"verdict"` // harsher | kinder | in_sync
	Compared     int               `json:"compared"`
	TotalRated   int               `json:"total_rated"` // movie and show ratings in the period
	LovedMore    []StatsCrowdEntry `json:"loved_more"`  // at most 3, gap > 0
	LovedLess    []StatsCrowdEntry `json:"loved_less"`  // at most 3, gap < 0
}

type StatsCrowdEntry struct {
	Title       StatsTitle `json:"title"`
	UserRating  int        `json:"user_rating"`
	TMDBAverage float64    `json:"tmdb_average"`
	TMDBVotes   int64      `json:"tmdb_votes"`
	Gap         float64    `json:"gap"` // user minus TMDB
}

type StatsUnratedMovies struct {
	Total  int                 `json:"total"`
	Recent []StatsUnratedMovie `json:"recent"` // at most 5, most recent play first
}

type StatsUnratedMovie struct {
	Movie        StatsTitle `json:"movie"`
	LastPlayedAt string     `json:"last_played_at"`
}

type StatsActivity struct {
	Months        []StatsMonthPlays `json:"months"` // exactly 12, oldest first
	Heatmap       StatsHeatmap      `json:"heatmap"`
	Weekday       [7]int            `json:"weekday"`     // Monday first
	TimeOfDay     *StatsTimeOfDay   `json:"time_of_day"` // null when no play has a real time of day
	LongestStreak int               `json:"longest_streak"`
	CurrentStreak *int              `json:"current_streak"` // null in a past year
	TrackingDays  *int              `json:"tracking_days"`  // all time only, once something was watched
	ActiveDays    *int              `json:"active_days"`    // a year only
	FirstPlay     *StatsFirstPlay   `json:"first_play"`     // null without a play
}

type StatsMonthPlays struct {
	Month string `json:"month"` // YYYY-MM in the user's zone
	Plays int    `json:"plays"`
}

// StatsHeatmap is one cell per day: 365 days ending today for all time, the
// whole calendar year otherwise (future days 0). Levels are 0 for no plays,
// else 1–4 by the quartiles of the active days' counts, the busiest day always 4.
type StatsHeatmap struct {
	Start  string `json:"start"` // YYYY-MM-DD of the first cell
	Plays  []int  `json:"plays"`
	Levels []int  `json:"levels"`
}

type StatsTimeOfDay struct {
	Morning   int    `json:"morning"`   // 05:00–11:59
	Afternoon int    `json:"afternoon"` // 12:00–16:59
	Evening   int    `json:"evening"`   // 17:00–21:59
	Night     int    `json:"night"`     // 22:00–04:59
	Dominant  string `json:"dominant"`  // morning | afternoon | evening | night; a tie goes to the earlier
}

type StatsFirstPlay struct {
	Title     StatsTitle       `json:"title"`   // the movie, or the episode's show
	Episode   *StatsEpisodeRef `json:"episode"` // null for a movie
	WatchedAt string           `json:"watched_at"`
}

type StatsEpisodeRef struct {
	Season int `json:"season"`
	Number int `json:"number"`
}

type StatsDecades struct {
	Buckets  []StatsDecade  `json:"buckets"`  // contiguous, oldest first, empty decades present as 0
	Favorite int            `json:"favorite"` // the decade's first year
	Oldest   *StatsEraTitle `json:"oldest"`
	Newest   *StatsEraTitle `json:"newest"`  // null when only one title has a year
	Covered  int            `json:"covered"` // titles with a year
	Total    int            `json:"total"`   // distinct watched titles
}

type StatsDecade struct {
	Decade int `json:"decade"`
	Titles int `json:"titles"`
}

type StatsEraTitle struct {
	Title StatsTitle `json:"title"`
	Year  int        `json:"year"`
}

type StatsRewatched struct {
	Title        StatsTitle `json:"title"`
	Plays        int        `json:"plays"`
	LastPlayedAt string     `json:"last_played_at"`
}

type StatsCompletion struct {
	Tracked           int                `json:"tracked"`
	Completed         int                `json:"completed"`
	InProgress        int                `json:"in_progress"`
	AverageCompletion float64            `json:"average_completion"` // 0..1 over shows with an aired episode
	AlmostThere       []StatsAlmostThere `json:"almost_there"`       // at most 3
}

type StatsAlmostThere struct {
	Show    StatsTitle `json:"show"`
	Watched int        `json:"watched"`
	Aired   int        `json:"aired"`
	Left    int        `json:"left"`
}

type StatsGenres struct {
	Entries []StatsGenre `json:"entries"` // at most 5
	Covered int          `json:"covered"` // titles with at least one genre
	Total   int          `json:"total"`   // distinct watched titles
}

type StatsGenre struct {
	Name   string `json:"name"`
	Titles int    `json:"titles"`
}

// StatsParams carries the query parameter as received; Stats defaults and
// validates it so the handler stays free of domain branching.
type StatsParams struct {
	Period string // "" | all | YYYY
}

// period is the resolved window. From and Until are RFC3339 UTC bounds, From
// inclusive and Until exclusive, empty for all time. Ref is the instant
// "recent" windows end at: now, or a past year's end.
type period struct {
	all   bool
	year  int
	from  string
	until string
	past  bool
	ref   time.Time
}

// label is the applied period as the API states it.
func (p period) label() string {
	if p.all {
		return "all"
	}
	return strconv.Itoa(p.year)
}

// calc is one request's shared inputs: the user's clock and zone (D-038), the
// resolved period, and the title memo every section resolves through.
type calc struct {
	loc    *time.Location
	now    time.Time // UTC
	today  string
	per    period
	titles *titleMemo
}

var yearPattern = regexp.MustCompile(`^[0-9]{4}$`)

const (
	trendWindowDays   = 90
	trendMinSample    = 3
	trendSteady       = 0.3
	crowdMinVotes     = 50
	crowdMinSample    = 5
	crowdInSync       = 0.5
	crowdListLimit    = 3
	topRatedLimit     = 10
	rewatchedLimit    = 5
	unratedLimit      = 5
	almostThereLimit  = 3
	genresLimit       = 5
	heatmapRollingDay = 365
	monthBuckets      = 12
)

// Stats computes the whole page for one user. A pure local read over the
// user's own log, ratings and show states — never a provider call, so it
// cannot 503. Every bucket (month, day, weekday, hour, streak) is measured in
// the user's zone, in Go, from one bulk read of the period's plays: no
// portable SQL knows a zone's offset on a given day (D-038).
func (s *Service) Stats(ctx context.Context, userID int64, p StatsParams) (*Stats, error) {
	wantAll, wantYear, err := parsePeriod(p.Period)
	if err != nil {
		return nil, err
	}

	clk, err := s.ClockFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	loc := clk.Zone
	now := clk.At
	currentYear := now.In(loc).Year()

	curFrom, curUntil, err := yearBounds(currentYear, loc)
	if err != nil {
		return nil, err
	}
	firstPlay, firstRating, playsThisYear, err := statsBounds(ctx, s.DB, userID, curFrom, curUntil)
	if err != nil {
		return nil, err
	}
	years := offeredYears(firstPlay, firstRating, currentYear, loc)

	var per period
	switch {
	case wantAll:
		per.all = true
	case wantYear != 0:
		per.year = wantYear
	case playsThisYear:
		per.year = currentYear
	default:
		per.all = true
	}
	per.ref = now
	if !per.all {
		per.from, per.until, err = yearBounds(per.year, loc)
		if err != nil {
			return nil, err
		}
		per.past = per.year < currentYear
		if per.past {
			// The window ends where the year does: the exclusive bound, which
			// under half-open comparison is the last instant of 31 December.
			until, err := time.Parse(time.RFC3339, per.until)
			if err != nil {
				return nil, err
			}
			per.ref = until
		}
	}

	c := &calc{loc: loc, now: now, today: clk.Today, per: per, titles: newTitleMemo(s)}

	plays, err := listPlays(ctx, s.DB, userID, per.from, per.until)
	if err != nil {
		return nil, err
	}
	fallbacks := map[int64]int{}
	if needsRuntimeFallback(plays) {
		docs, err := listShowRuntimeFallbacks(ctx, s.DB, userID, per.from, per.until)
		if err != nil {
			return nil, err
		}
		fallbacks = showRuntimeFallbacks(docs)
	}
	ratings, err := listRatings(ctx, s.DB, userID, per.from, per.until)
	if err != nil {
		return nil, err
	}
	var snapshots []titleDoc
	if hasTitleRating(ratings) {
		snapshots, err = listRatedSnapshots(ctx, s.DB, userID, per.from, per.until)
		if err != nil {
			return nil, err
		}
	}

	out := &Stats{Period: per.label(), Years: years, Today: clk.Today}
	titles := aggregateTitles(plays)

	out.Headline = c.headline(plays, fallbacks)
	if out.MostWatchedShow, err = c.mostWatchedShow(ctx, plays, fallbacks); err != nil {
		return nil, err
	}
	out.Ratings = ratingsSummary(ratings)
	if out.TopRated, err = c.topRated(ctx, ratings); err != nil {
		return nil, err
	}
	out.RatingTrend = c.ratingTrend(ratings)
	if out.Crowd, err = c.crowd(ctx, ratings, snapshots); err != nil {
		return nil, err
	}
	if out.UnratedMovies, err = c.unratedMovies(ctx, titles, ratings); err != nil {
		return nil, err
	}
	if out.Activity, err = c.activity(ctx, plays); err != nil {
		return nil, err
	}
	if out.Decades, err = c.decades(ctx, titles); err != nil {
		return nil, err
	}
	if out.MostRewatched, err = c.mostRewatched(ctx, titles); err != nil {
		return nil, err
	}
	if per.all {
		rows, err := listCompletion(ctx, s.DB, userID, clk.Today)
		if err != nil {
			return nil, err
		}
		if out.Completion, err = c.completion(ctx, rows); err != nil {
			return nil, err
		}
	}
	out.Genres = genres(titles)
	return out, nil
}

// parsePeriod reads the period parameter: "" means "default", "all" means all
// time, a four-digit year means that year. Anything else is a 422, before any
// store call.
func parsePeriod(raw string) (all bool, year int, err error) {
	switch {
	case raw == "":
		return false, 0, nil
	case raw == "all":
		return true, 0, nil
	case yearPattern.MatchString(raw):
		y, _ := strconv.Atoi(raw)
		return false, y, nil
	default:
		return false, 0, fmt.Errorf("%w: period must be all or a four-digit year", httpx.ErrValidation)
	}
}

// yearBounds is the half-open window of one calendar year in loc, as the
// RFC3339 UTC strings the _at columns compare against: local midnight on
// 1 January of the year and of the next one.
func yearBounds(year int, loc *time.Location) (from, until string, err error) {
	from, err = tz.Midnight(fmt.Sprintf("%04d-01-01", year), loc)
	if err != nil {
		return "", "", err
	}
	until, err = tz.Midnight(fmt.Sprintf("%04d-01-01", year+1), loc)
	if err != nil {
		return "", "", err
	}
	return from, until, nil
}

// offeredYears lists every year from the earlier of the first play and the
// first rating (each as a year in loc) up to the current one, newest first.
// A clock set back before the first year still offers that year; nothing
// recorded offers nothing — an empty list, never null.
func offeredYears(firstPlay, firstRating *string, currentYear int, loc *time.Location) []int {
	first := 0
	for _, at := range []*string{firstPlay, firstRating} {
		if at == nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			continue
		}
		if y := t.In(loc).Year(); first == 0 || y < first {
			first = y
		}
	}
	if first == 0 {
		return []int{}
	}
	hi, lo := currentYear, first
	if first > hi {
		hi, lo = first, currentYear
	}
	out := make([]int, 0, hi-lo+1)
	for y := hi; y >= lo; y-- {
		out = append(out, y)
	}
	return out
}

// needsRuntimeFallback reports whether any episode play lacks a runtime of its
// own, which is the only case the show's regular runtime is read for.
func needsRuntimeFallback(plays []playRow) bool {
	for _, r := range plays {
		if r.TargetType == "episode" && r.EpisodeRuntime == nil {
			return true
		}
	}
	return false
}

// hasTitleRating reports whether any movie or show rating is present — what
// the crowd comparison needs a stored document for.
func hasTitleRating(ratings []ratingRow) bool {
	for _, r := range ratings {
		if r.TargetType == "movie" || r.TargetType == "show" {
			return true
		}
	}
	return false
}

// parseInstant reads a stored RFC3339 UTC instant. Every row here was written
// by Go, so a failure is a corrupt row; it is reported as the zero time and the
// callers skip the row rather than fail the page.
func parseInstant(at string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// localDay is the calendar day an instant falls on in the user's zone.
func (c *calc) localDay(at string) (string, bool) {
	t, ok := parseInstant(at)
	if !ok {
		return "", false
	}
	return tz.Day(t, c.loc), true
}

// localMonth is the YYYY-MM month an instant falls in, in the user's zone.
func (c *calc) localMonth(at string) (string, bool) {
	t, ok := parseInstant(at)
	if !ok {
		return "", false
	}
	return t.In(c.loc).Format("2006-01"), true
}

// monthWindow is the page's twelve month buckets, oldest first: the twelve
// ending with the current month for all time, January to December for a year.
// Each is built with time.Date in the user's zone, which normalises a negative
// month, so no bucket can land on a wrong month across a year boundary.
func (c *calc) monthWindow() []string {
	out := make([]string, 0, monthBuckets)
	if c.per.all {
		local := c.now.In(c.loc)
		for i := 0; i < monthBuckets; i++ {
			out = append(out, time.Date(local.Year(), local.Month()-monthBuckets+1+time.Month(i), 1, 0, 0, 0, 0, c.loc).Format("2006-01"))
		}
		return out
	}
	for m := time.January; m <= time.December; m++ {
		out = append(out, time.Date(c.per.year, m, 1, 0, 0, 0, 0, c.loc).Format("2006-01"))
	}
	return out
}

// roundTenth rounds half away from zero to one decimal. The result is the
// nearest representable tenth, so comparing it against a tenth-valued
// threshold with <= / >= is exact.
func roundTenth(v float64) float64 {
	return math.Round(v*10) / 10
}
