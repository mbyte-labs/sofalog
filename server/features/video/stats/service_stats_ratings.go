package stats

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"
)

// ratingsSummary is the "Your ratings" section over every rating in the
// period: the 1–10 distribution, the count, the overall mean and one mean per
// kind (seasons are not rateable, D-020, so there are three). Null without a
// rating.
func ratingsSummary(ratings []ratingRow) *StatsRatings {
	if len(ratings) == 0 {
		return nil
	}
	out := &StatsRatings{Count: len(ratings)}
	sums := map[string]int{}
	counts := map[string]int{}
	total := 0
	for _, r := range ratings {
		if r.Rating >= 1 && r.Rating <= 10 {
			out.Distribution[r.Rating-1]++
		}
		total += r.Rating
		sums[r.TargetType] += r.Rating
		counts[r.TargetType]++
	}
	out.Average = float64(total) / float64(len(ratings))
	mean := func(kind string) *float64 {
		if counts[kind] == 0 {
			return nil
		}
		v := float64(sums[kind]) / float64(counts[kind])
		return &v
	}
	out.MoviesAverage = mean("movie")
	out.ShowsAverage = mean("show")
	out.EpisodesAverage = mean("episode")
	return out
}

// titleRatings keeps the movie and show ratings — what the highest-rated rail,
// the trend and the crowd comparison are over. Episode ratings are left out of
// all three: episodes are rated in binge bursts, so they would track what was
// being watched rather than how it was judged.
func titleRatings(ratings []ratingRow) []ratingRow {
	var out []ratingRow
	for _, r := range ratings {
		if r.TargetType == "movie" || r.TargetType == "show" {
			out = append(out, r)
		}
	}
	return out
}

// topRated lists the highest-rated movies and shows: rating first, then the
// most recently rated, then kind and id, at most ten. Empty, never null.
func (c *calc) topRated(ctx context.Context, ratings []ratingRow) ([]StatsTopRated, error) {
	rows := titleRatings(ratings)
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Rating != b.Rating {
			return a.Rating > b.Rating
		}
		if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		return lessKey(titleKey{a.TargetType, a.TargetID}, titleKey{b.TargetType, b.TargetID})
	})
	out := make([]StatsTopRated, 0, topRatedLimit)
	for _, r := range rows {
		if len(out) == topRatedLimit {
			break
		}
		title, err := c.titles.get(ctx, r.TargetType, r.TargetID)
		if err != nil {
			return nil, err
		}
		if title == nil {
			continue
		}
		out = append(out, StatsTopRated{Title: *title, Rating: r.Rating, RatedAt: r.UpdatedAt})
	}
	return out, nil
}

// ratingTrend is "Ratings over time" for movie and show ratings: the mean per
// month over the page's twelve months, and the recent window (the 90 days
// before the reference — now, or a past year's end) against everything
// earlier in the period. The delta is stated only with at least three ratings
// a side, rounded to one decimal BEFORE the verdict so the number shown and
// the sentence can never disagree. Null without a movie or show rating.
func (c *calc) ratingTrend(ratings []ratingRow) *StatsRatingTrend {
	rows := titleRatings(ratings)
	if len(rows) == 0 {
		return nil
	}
	window := c.monthWindow()
	index := make(map[string]int, len(window))
	months := make([]StatsRatingMonth, 0, len(window))
	sums := make([]int, len(window))
	for i, m := range window {
		index[m] = i
		months = append(months, StatsRatingMonth{Month: m})
	}
	recentFrom := c.per.ref.Add(-trendWindowDays * 24 * time.Hour)
	var recent, earlier StatsRatingSample
	recentSum, earlierSum := 0, 0
	for _, r := range rows {
		if m, ok := c.localMonth(r.UpdatedAt); ok {
			if i, ok := index[m]; ok {
				months[i].Count++
				sums[i] += r.Rating
			}
		}
		t, ok := parseInstant(r.UpdatedAt)
		if !ok {
			continue
		}
		switch {
		case !t.Before(recentFrom) && t.Before(c.per.ref):
			recent.Count++
			recentSum += r.Rating
		case t.Before(recentFrom):
			earlier.Count++
			earlierSum += r.Rating
		}
	}
	for i := range months {
		if months[i].Count > 0 {
			v := float64(sums[i]) / float64(months[i].Count)
			months[i].Average = &v
		}
	}
	if recent.Count > 0 {
		v := float64(recentSum) / float64(recent.Count)
		recent.Average = &v
	}
	if earlier.Count > 0 {
		v := float64(earlierSum) / float64(earlier.Count)
		earlier.Average = &v
	}
	out := &StatsRatingTrend{Months: months, Recent: recent, Earlier: earlier, ReferenceAt: c.per.ref.UTC().Format(time.RFC3339)}
	if recent.Count >= trendMinSample && earlier.Count >= trendMinSample {
		delta := roundTenth(*recent.Average - *earlier.Average)
		verdict := "steady"
		switch {
		case delta <= -trendSteady:
			verdict = "harsher"
		case delta >= trendSteady:
			verdict = "kinder"
		}
		out.Delta, out.Verdict = &delta, &verdict
	}
	return out
}

// voteFacts is what the crowd comparison reads out of a stored TMDB body.
type voteFacts struct {
	VoteAverage float64 `json:"vote_average"`
	VoteCount   int64   `json:"vote_count"`
}

// crowd is "You vs the crowd": the user's movie and show ratings against
// TMDB's averages out of the stored snapshots — never a fetch. A title is
// compared only when its average rests on at least 50 votes; the section needs
// five compared titles. Both averages are over the compared titles only, so
// the delta is also the mean per-title gap. The two lists hold the titles the
// user liked more / less than the crowd (an exact match in neither), largest
// gap first, then more votes, then kind and id, at most three each.
func (c *calc) crowd(ctx context.Context, ratings []ratingRow, docs []titleDoc) (*StatsCrowd, error) {
	rows := titleRatings(ratings)
	facts := make(map[titleKey]voteFacts, len(docs))
	for _, d := range docs {
		var v voteFacts
		if err := json.Unmarshal([]byte(d.MetadataJSON), &v); err != nil {
			continue
		}
		facts[titleKey{d.Kind, d.ID}] = v
	}
	type compared struct {
		key   titleKey
		user  int
		tmdb  voteFacts
		gap   float64
		absGp float64
	}
	var cmp []compared
	userSum, crowdSum := 0.0, 0.0
	for _, r := range rows {
		key := titleKey{r.TargetType, r.TargetID}
		f, ok := facts[key]
		if !ok || f.VoteCount < crowdMinVotes || f.VoteAverage <= 0 {
			continue
		}
		gap := float64(r.Rating) - f.VoteAverage
		cmp = append(cmp, compared{key: key, user: r.Rating, tmdb: f, gap: gap, absGp: math.Abs(gap)})
		userSum += float64(r.Rating)
		crowdSum += f.VoteAverage
	}
	if len(cmp) < crowdMinSample {
		return nil, nil
	}
	n := float64(len(cmp))
	out := &StatsCrowd{
		UserAverage: userSum / n, CrowdAverage: crowdSum / n,
		Compared: len(cmp), TotalRated: len(rows),
		LovedMore: make([]StatsCrowdEntry, 0, crowdListLimit),
		LovedLess: make([]StatsCrowdEntry, 0, crowdListLimit),
	}
	out.Delta = out.UserAverage - out.CrowdAverage
	switch {
	case out.Delta <= -crowdInSync+1e-9:
		out.Verdict = "harsher"
	case out.Delta >= crowdInSync-1e-9:
		out.Verdict = "kinder"
	default:
		out.Verdict = "in_sync"
	}
	sort.Slice(cmp, func(i, j int) bool {
		a, b := cmp[i], cmp[j]
		if a.absGp != b.absGp {
			return a.absGp > b.absGp
		}
		if a.tmdb.VoteCount != b.tmdb.VoteCount {
			return a.tmdb.VoteCount > b.tmdb.VoteCount
		}
		return lessKey(a.key, b.key)
	})
	for _, e := range cmp {
		var list *[]StatsCrowdEntry
		switch {
		case e.gap > 0:
			list = &out.LovedMore
		case e.gap < 0:
			list = &out.LovedLess
		default:
			continue
		}
		if len(*list) == crowdListLimit {
			continue
		}
		title, err := c.titles.get(ctx, e.key.Kind, e.key.ID)
		if err != nil {
			return nil, err
		}
		if title == nil {
			continue
		}
		*list = append(*list, StatsCrowdEntry{Title: *title, UserRating: e.user, TMDBAverage: e.tmdb.VoteAverage, TMDBVotes: e.tmdb.VoteCount, Gap: e.gap})
	}
	return out, nil
}
