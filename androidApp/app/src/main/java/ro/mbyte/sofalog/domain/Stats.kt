package ro.mbyte.sofalog.domain

import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.Year
import java.time.YearMonth
import java.time.ZoneId
import java.time.temporal.ChronoUnit
import kotlin.math.abs
import kotlin.math.floor
import kotlin.math.sign

/*
 * The Stats page's fold (SPEC D-039), ported from the server's `features/video/stats` section for
 * section: same constants, same orderings, same tie-breaks. It is a pure function over the
 * device's own rows — never a provider call — and it is held to the Go implementation by the
 * shared cases in `fixtures/video-rules/stats.json`: change a rule here, change the fixture and
 * the server with it.
 *
 * Every bucket (month, day, weekday, hour, streak) is the instant read in the user's zone (D-038);
 * day arithmetic runs on calendar days, so a DST change can never make one 23 or 25 hours long.
 *
 * Where the server runs each query per period, this reads everything once and applies the period
 * here, over the rows' own instants — the same half-open window, compared as the same strings.
 */

private const val TREND_WINDOW_DAYS = 90L
private const val TREND_MIN_SAMPLE = 3
private const val TREND_STEADY = 0.3
private const val CROWD_MIN_VOTES = 50L
private const val CROWD_MIN_SAMPLE = 5
private const val CROWD_IN_SYNC = 0.5
private const val CROWD_LIST_LIMIT = 3
private const val TOP_RATED_LIMIT = 10
private const val REWATCHED_LIMIT = 5
private const val UNRATED_LIMIT = 5
private const val ALMOST_THERE_LIMIT = 3
private const val GENRES_LIMIT = 5
private const val HEATMAP_ROLLING_DAYS = 365
private const val MONTH_BUCKETS = 12

private val YEAR_PATTERN = Regex("^[0-9]{4}$")

/** Whether [raw] is a period the page can ask for: nothing (the default), "all", or a four-digit year. */
fun isStatsPeriod(raw: String?): Boolean =
    raw.isNullOrEmpty() || raw == STATS_PERIOD_ALL || YEAR_PATTERN.matches(raw)

/**
 * The whole page for one [period] — null or "" for the default (the current year when it holds a
 * play, else all time), "all", or a four-digit year in [zone]. Anything else throws
 * [IllegalArgumentException], the server's 422. A year nothing happened in is computed and empty.
 */
fun computeStats(input: StatsInput, period: String?, now: Instant, zone: ZoneId): Stats {
    require(isStatsPeriod(period)) { "period must be all or a four-digit year" }
    val wantAll = period == STATS_PERIOD_ALL
    val wantYear = if (period.isNullOrEmpty() || wantAll) 0 else period.toInt()

    val today = LocalDate.ofInstant(now, zone)
    val currentYear = today.year
    val (curFrom, curUntil) = yearBounds(currentYear, zone)

    // The bounds read the raw log: a row whose target is gone still dates the first play.
    val firstPlay = input.plays.minOfOrNull { it.watchedAt }
    val firstRating = input.ratings.minOfOrNull { it.updatedAt }
    val playsThisYear = input.plays.any { it.watchedAt >= curFrom && it.watchedAt < curUntil }
    val years = offeredYears(firstPlay, firstRating, currentYear, zone)

    val all = wantAll || (wantYear == 0 && !playsThisYear)
    val year = when {
        all -> 0
        wantYear != 0 -> wantYear
        else -> currentYear
    }
    var from = ""
    var until = ""
    var past = false
    var ref = now
    if (!all) {
        val bounds = yearBounds(year, zone)
        from = bounds.first
        until = bounds.second
        past = year < currentYear
        // A past year's "recent" window ends where the year does: its exclusive bound.
        if (past) ref = LocalDate.of(year + 1, 1, 1).atStartOfDay(zone).toInstant()
    }
    val calc = Calc(zone, now, today, all, year, past, ref, input.titles)

    fun inPeriod(at: String) = all || (at >= from && at < until)
    val plays = input.plays
        .filter { it.present && inPeriod(it.watchedAt) }
        .sortedWith(compareBy<StatsPlay> { it.watchedAt }.thenBy { it.id })
    val ratings = input.ratings
        .filter { it.present && inPeriod(it.updatedAt) }
        .sortedWith(compareBy<StatsRating> { it.updatedAt }.thenBy { it.targetType }.thenBy { it.targetId })
    val fallbacks = input.showRuntimeFallback
    val titles = aggregateTitles(plays)

    return Stats(
        period = if (all) STATS_PERIOD_ALL else year.toString(),
        years = years,
        today = today.toString(),
        headline = headline(plays, fallbacks),
        mostWatchedShow = calc.mostWatchedShow(plays, fallbacks),
        ratings = ratingsSummary(ratings),
        topRated = calc.topRated(ratings),
        ratingTrend = calc.ratingTrend(ratings),
        crowd = calc.crowd(ratings, input.voteFacts),
        unratedMovies = calc.unratedMovies(titles, ratings),
        activity = calc.activity(plays),
        decades = calc.decades(titles),
        mostRewatched = calc.mostRewatched(titles),
        completion = if (all) calc.completion(input.completion) else null,
        genres = genres(titles),
    )
}

/** One calendar year in [zone] as the half-open pair of stored instants the log compares against. */
private fun yearBounds(year: Int, zone: ZoneId): Pair<String, String> =
    midnight(year, zone) to midnight(year + 1, zone)

private fun midnight(year: Int, zone: ZoneId): String =
    LocalDate.of(year, 1, 1).atStartOfDay(zone).toInstant().toString()

/**
 * Every year from the earlier of the first play and the first rating up to the current one,
 * newest first. A clock set back before the first year still offers that year; nothing recorded
 * offers nothing.
 */
private fun offeredYears(firstPlay: String?, firstRating: String?, currentYear: Int, zone: ZoneId): List<Int> {
    var first = 0
    for (at in listOf(firstPlay, firstRating)) {
        val y = at?.let(::parseInstant)?.atZone(zone)?.year ?: continue
        if (first == 0 || y < first) first = y
    }
    if (first == 0) return emptyList()
    return (maxOf(currentYear, first) downTo minOf(currentYear, first)).toList()
}

private fun parseInstant(at: String): Instant? = runCatching { Instant.parse(at) }.getOrNull()

/** Half away from zero, to one decimal — the server's `math.Round`, which `kotlin.math.round` (half to even) is not. */
internal fun roundTenth(v: Double): Double = sign(v) * floor(abs(v) * 10 + 0.5) / 10

/** The year of a YYYY-MM-DD date — a movie's release, a show's first air; null when undated or unreadable. */
fun statsYear(date: String?): Int? =
    date?.takeIf { it.length >= 4 }?.substring(0, 4)?.toIntOrNull()?.takeIf { it > 0 }

/** One distinct watched title: a movie, or a show every one of its episode plays rolls up to. */
private data class TitleAgg(
    val key: StatsTitleKey,
    var plays: Int,
    var lastAt: String,
    val year: Int?,
    val genres: List<String>?,
)

/** Kind, then id: the deterministic tail of every ordering. */
private val byKey: Comparator<StatsTitleKey> = compareBy<StatsTitleKey> { it.kind }.thenBy { it.id }

/** Distinct titles in first-seen order. Rows arrive oldest first, so the last play seen is the latest. */
private fun aggregateTitles(plays: List<StatsPlay>): List<TitleAgg> {
    val out = LinkedHashMap<StatsTitleKey, TitleAgg>()
    for (r in plays) {
        val agg = if (r.targetType == TARGET_EPISODE_TYPE) {
            val showId = r.showId ?: continue
            out.getOrPut(StatsTitleKey(STATS_SHOW, showId)) {
                TitleAgg(StatsTitleKey(STATS_SHOW, showId), 0, "", statsYear(r.showFirstAir), r.showGenres)
            }
        } else {
            out.getOrPut(StatsTitleKey(STATS_MOVIE, r.targetId)) {
                TitleAgg(StatsTitleKey(STATS_MOVIE, r.targetId), 0, "", statsYear(r.movieRelease), r.movieGenres)
            }
        }
        agg.plays++
        agg.lastAt = r.watchedAt
    }
    return out.values.toList()
}

private const val TARGET_MOVIE_TYPE = "movie"
private const val TARGET_EPISODE_TYPE = "episode"

/** A play's runtime: the movie's, or the episode's falling back to its show's regular one, else 0. */
private fun minutesOf(r: StatsPlay, fallbacks: Map<Long, Int>): Int =
    if (r.targetType == TARGET_MOVIE_TYPE) {
        r.movieRuntime ?: 0
    } else {
        r.episodeRuntime ?: r.showId?.let { fallbacks[it] } ?: 0
    }

/** Movies and episodes are DISTINCT targets, so a rewatch counts once; an unknown runtime counts 0. */
private fun headline(plays: List<StatsPlay>, fallbacks: Map<Long, Int>): StatsHeadline {
    val movies = HashSet<Long>()
    val episodes = HashSet<Long>()
    var movieMinutes = 0
    var episodeMinutes = 0
    for (r in plays) {
        if (r.targetType == TARGET_MOVIE_TYPE) {
            movies += r.targetId
            movieMinutes += minutesOf(r, fallbacks)
        } else {
            episodes += r.targetId
            episodeMinutes += minutesOf(r, fallbacks)
        }
    }
    return StatsHeadline(
        hours = (movieMinutes + episodeMinutes) / 60,
        plays = plays.size,
        movies = movies.size,
        episodes = episodes.size,
        movieMinutes = movieMinutes,
        episodeMinutes = episodeMinutes,
    )
}

/** Every rating in the period: the 1–10 distribution, the mean, and one mean per kind. */
private fun ratingsSummary(ratings: List<StatsRating>): StatsRatings? {
    if (ratings.isEmpty()) return null
    val distribution = IntArray(10)
    for (r in ratings) if (r.rating in 1..10) distribution[r.rating - 1]++
    fun mean(kind: String): Double? =
        ratings.filter { it.targetType == kind }.takeIf { it.isNotEmpty() }
            ?.let { rows -> rows.sumOf { it.rating }.toDouble() / rows.size }
    return StatsRatings(
        distribution = distribution.toList(),
        count = ratings.size,
        average = ratings.sumOf { it.rating }.toDouble() / ratings.size,
        moviesAverage = mean(STATS_MOVIE),
        showsAverage = mean(STATS_SHOW),
        episodesAverage = mean(TARGET_EPISODE_TYPE),
    )
}

/**
 * Movie and show ratings — what the highest-rated rail, the trend and the crowd comparison are
 * over. Episodes are rated in binge bursts, so they would track what was watched, not how it was judged.
 */
private fun titleRatings(ratings: List<StatsRating>): List<StatsRating> =
    ratings.filter { it.targetType == STATS_MOVIE || it.targetType == STATS_SHOW }

private val StatsRating.key get() = StatsTitleKey(targetType, targetId)

/**
 * How many distinct titles list each genre — each title once per name. Most titles first, then
 * name, at most five. Null when the period holds no title; an empty list with titles present is a
 * legal answer.
 */
private fun genres(titles: List<TitleAgg>): StatsGenres? {
    if (titles.isEmpty()) return null
    val counts = HashMap<String, Int>()
    var covered = 0
    for (t in titles) {
        val names = t.genres?.filter { it.isNotEmpty() }?.distinct().orEmpty()
        if (names.isEmpty()) continue
        names.forEach { counts[it] = (counts[it] ?: 0) + 1 }
        covered++
    }
    val entries = counts.map { StatsGenre(it.key, it.value) }
        .sortedWith(compareByDescending<StatsGenre> { it.titles }.thenBy { it.name })
        .take(GENRES_LIMIT)
    return StatsGenres(entries, covered, titles.size)
}

/**
 * Shades 0..4: 0 for none; otherwise 1 plus the number of quartile thresholds strictly below the
 * count — the thresholds being the sorted active counts at ⌊n·k/4⌋ — and the busiest day forced to
 * 4. Quartiles of the ACTIVE days, so one bulk-import day does not wash out every other.
 */
internal fun heatLevels(plays: List<Int>): List<Int> {
    val active = plays.filter { it > 0 }.sorted()
    if (active.isEmpty()) return plays.map { 0 }
    val max = active.last()
    val n = active.size
    val thresholds = listOf(active[n / 4], active[n * 2 / 4], active[n * 3 / 4])
    return plays.map { count ->
        when (count) {
            0 -> 0
            max -> 4
            else -> 1 + thresholds.count { it < count }
        }
    }
}

/** One request's shared facts: the user's clock and zone, the resolved period, the title lookup. */
private class Calc(
    val zone: ZoneId,
    val now: Instant,
    val today: LocalDate,
    val all: Boolean,
    val year: Int,
    val past: Boolean,
    val ref: Instant,
    val titles: Map<StatsTitleKey, StatsTitle>,
) {
    fun title(key: StatsTitleKey): StatsTitle? = titles[key]
    fun movie(id: Long): StatsTitle? = titles[StatsTitleKey(STATS_MOVIE, id)]
    fun show(id: Long): StatsTitle? = titles[StatsTitleKey(STATS_SHOW, id)]

    fun localDay(at: String): LocalDate? = parseInstant(at)?.atZone(zone)?.toLocalDate()

    fun localMonth(at: String): String? = parseInstant(at)?.atZone(zone)?.let { YearMonth.from(it).toString() }

    /** The page's twelve months, oldest first: the twelve ending now for all time, the year's otherwise. */
    fun monthWindow(): List<String> {
        val first = if (all) YearMonth.from(today).minusMonths(MONTH_BUCKETS - 1L) else YearMonth.of(year, 1)
        return (0 until MONTH_BUCKETS).map { first.plusMonths(it.toLong()).toString() }
    }

    /** The show with the most minutes, then plays, then the most recent play, then the lowest id. */
    fun mostWatchedShow(plays: List<StatsPlay>, fallbacks: Map<Long, Int>): StatsMostWatchedShow? {
        class Agg(val id: Long, var minutes: Int = 0, var plays: Int = 0, var last: String = "")
        val shows = LinkedHashMap<Long, Agg>()
        for (r in plays) {
            if (r.targetType != TARGET_EPISODE_TYPE) continue
            val showId = r.showId ?: continue
            val agg = shows.getOrPut(showId) { Agg(showId) }
            agg.minutes += minutesOf(r, fallbacks)
            agg.plays++
            agg.last = r.watchedAt
        }
        val ordered = shows.values.sortedWith(
            compareByDescending<Agg> { it.minutes }.thenByDescending { it.plays }
                .thenByDescending { it.last }.thenBy { it.id },
        )
        for (sh in ordered) {
            val title = show(sh.id) ?: continue
            return StatsMostWatchedShow(title, sh.minutes, sh.plays, sh.last)
        }
        return null
    }

    /** Rating first, then the most recently rated, then kind and id, at most ten. */
    fun topRated(ratings: List<StatsRating>): List<StatsTopRated> =
        titleRatings(ratings)
            .sortedWith(
                compareByDescending<StatsRating> { it.rating }.thenByDescending { it.updatedAt }
                    .thenComparing({ it.key }, byKey),
            )
            .asSequence()
            .mapNotNull { r -> title(r.key)?.let { StatsTopRated(it, r.rating, r.updatedAt) } }
            .take(TOP_RATED_LIMIT)
            .toList()

    /**
     * The mean per month over the page's twelve months, and the recent window (the 90 days before
     * the reference) against everything earlier in the period. The delta is stated only with three
     * ratings a side, and rounded BEFORE the verdict so the number and the sentence cannot disagree.
     */
    fun ratingTrend(ratings: List<StatsRating>): StatsRatingTrend? {
        val rows = titleRatings(ratings)
        if (rows.isEmpty()) return null
        val window = monthWindow()
        val index = window.withIndex().associate { it.value to it.index }
        val counts = IntArray(window.size)
        val sums = IntArray(window.size)
        val recentFrom = ref.minus(Duration.ofHours(TREND_WINDOW_DAYS * 24))
        var recentCount = 0
        var recentSum = 0
        var earlierCount = 0
        var earlierSum = 0
        for (r in rows) {
            localMonth(r.updatedAt)?.let(index::get)?.let { i ->
                counts[i]++
                sums[i] += r.rating
            }
            val t = parseInstant(r.updatedAt) ?: continue
            when {
                !t.isBefore(recentFrom) && t.isBefore(ref) -> {
                    recentCount++
                    recentSum += r.rating
                }
                t.isBefore(recentFrom) -> {
                    earlierCount++
                    earlierSum += r.rating
                }
            }
        }
        fun mean(sum: Int, count: Int): Double? = if (count > 0) sum.toDouble() / count else null
        val recent = StatsRatingSample(recentCount, mean(recentSum, recentCount))
        val earlier = StatsRatingSample(earlierCount, mean(earlierSum, earlierCount))
        var delta: Double? = null
        var verdict: String? = null
        if (recentCount >= TREND_MIN_SAMPLE && earlierCount >= TREND_MIN_SAMPLE) {
            val d = roundTenth(recent.average!! - earlier.average!!)
            delta = d
            verdict = when {
                d <= -TREND_STEADY -> "harsher"
                d >= TREND_STEADY -> "kinder"
                else -> "steady"
            }
        }
        return StatsRatingTrend(
            months = window.mapIndexed { i, m -> StatsRatingMonth(m, counts[i], mean(sums[i], counts[i])) },
            recent = recent,
            earlier = earlier,
            delta = delta,
            verdict = verdict,
            referenceAt = ref.truncatedTo(ChronoUnit.SECONDS).toString(),
        )
    }

    /**
     * The user's movie and show ratings against TMDB's averages out of the stored bodies. A title
     * is compared only when its average rests on at least 50 votes; the section needs five. Both
     * averages are over the compared titles only. The lists hold the largest gaps first, then more
     * votes, then kind and id, at most three each; an exact match is in neither.
     */
    fun crowd(ratings: List<StatsRating>, facts: Map<StatsTitleKey, StatsVoteFacts>): StatsCrowd? {
        class Compared(val key: StatsTitleKey, val user: Int, val tmdb: StatsVoteFacts, val gap: Double)
        val rows = titleRatings(ratings)
        val cmp = rows.mapNotNull { r ->
            val f = facts[r.key] ?: return@mapNotNull null
            if (f.voteCount < CROWD_MIN_VOTES || f.voteAverage <= 0) return@mapNotNull null
            Compared(r.key, r.rating, f, r.rating - f.voteAverage)
        }
        if (cmp.size < CROWD_MIN_SAMPLE) return null
        val userAverage = cmp.sumOf { it.user.toDouble() } / cmp.size
        val crowdAverage = cmp.sumOf { it.tmdb.voteAverage } / cmp.size
        val delta = userAverage - crowdAverage
        val verdict = when {
            delta <= -CROWD_IN_SYNC + 1e-9 -> "harsher"
            delta >= CROWD_IN_SYNC - 1e-9 -> "kinder"
            else -> "in_sync"
        }
        val lovedMore = ArrayList<StatsCrowdEntry>()
        val lovedLess = ArrayList<StatsCrowdEntry>()
        val ordered = cmp.sortedWith(
            compareByDescending<Compared> { abs(it.gap) }.thenByDescending { it.tmdb.voteCount }
                .thenComparing({ it.key }, byKey),
        )
        for (e in ordered) {
            val list = when {
                e.gap > 0 -> lovedMore
                e.gap < 0 -> lovedLess
                else -> continue
            }
            if (list.size == CROWD_LIST_LIMIT) continue
            val title = title(e.key) ?: continue
            list += StatsCrowdEntry(title, e.user, e.tmdb.voteAverage, e.tmdb.voteCount, e.gap)
        }
        return StatsCrowd(userAverage, crowdAverage, delta, verdict, cmp.size, rows.size, lovedMore, lovedLess)
    }

    /**
     * The watched movies with no movie rating IN THE PERIOD — in a year, "rated" means rated that
     * year — most recent play first, then lowest id. The total counts all; the list carries five.
     */
    fun unratedMovies(titles: List<TitleAgg>, ratings: List<StatsRating>): StatsUnratedMovies? {
        val rated = ratings.filter { it.targetType == STATS_MOVIE }.map { it.targetId }.toSet()
        val movies = titles.filter { it.key.kind == STATS_MOVIE && it.key.id !in rated }
            .sortedWith(compareByDescending<TitleAgg> { it.lastAt }.thenBy { it.key.id })
        if (movies.isEmpty()) return null
        val recent = movies.asSequence()
            .mapNotNull { m -> movie(m.key.id)?.let { StatsUnratedMovie(it, m.lastAt) } }
            .take(UNRATED_LIMIT)
            .toList()
        return StatsUnratedMovies(movies.size, recent)
    }

    fun activity(plays: List<StatsPlay>): StatsActivity {
        val heatmap = heatmap(plays)
        val (weekday, timeOfDay) = weekdayAndTimeOfDay(plays)
        val (longest, current) = streaks(plays)
        val trackingDays = if (all && plays.isNotEmpty()) {
            localDay(plays.first().watchedAt)?.let { ChronoUnit.DAYS.between(it, today).toInt().coerceAtLeast(1) }
        } else {
            null
        }
        return StatsActivity(
            months = monthlyPlays(plays),
            heatmap = heatmap,
            weekday = weekday,
            timeOfDay = timeOfDay,
            longestStreak = longest,
            currentStreak = current,
            trackingDays = trackingDays,
            activeDays = if (all) null else heatmap.plays.count { it > 0 },
            firstPlay = firstPlay(plays),
        )
    }

    /** A play outside the twelve months (all time, older than a year) is simply in no bucket. */
    private fun monthlyPlays(plays: List<StatsPlay>): List<StatsMonthPlays> {
        val window = monthWindow()
        val index = window.withIndex().associate { it.value to it.index }
        val counts = IntArray(window.size)
        for (r in plays) localMonth(r.watchedAt)?.let(index::get)?.let { counts[it]++ }
        return window.mapIndexed { i, m -> StatsMonthPlays(m, counts[i]) }
    }

    /** One cell per day: the 365 days ending today for all time, the whole calendar year otherwise. */
    private fun heatmap(plays: List<StatsPlay>): StatsHeatmap {
        val start = if (all) today.minusDays(HEATMAP_ROLLING_DAYS - 1L) else LocalDate.of(year, 1, 1)
        val n = if (all) HEATMAP_ROLLING_DAYS else Year.of(year).length()
        val counts = IntArray(n)
        for (r in plays) {
            val day = localDay(r.watchedAt) ?: continue
            val i = ChronoUnit.DAYS.between(start, day)
            if (i in 0 until n) counts[i.toInt()]++
        }
        val list = counts.toList()
        return StatsHeatmap(start.toString(), list, heatLevels(list))
    }

    /**
     * Plays by local weekday (Monday first) and by part of the local day. A play at EXACTLY local
     * midnight is left out of the time of day: the bulk "on its air date" marks stamp that instant,
     * which would make everyone a night owl. The dominant part is the fullest; a tie goes to the earlier.
     */
    private fun weekdayAndTimeOfDay(plays: List<StatsPlay>): Pair<List<Int>, StatsTimeOfDay?> {
        val weekday = IntArray(7)
        val parts = IntArray(4)
        var timed = 0
        for (r in plays) {
            val at = parseInstant(r.watchedAt)?.atZone(zone) ?: continue
            weekday[at.dayOfWeek.value - 1]++
            if (at.toLocalTime() == LocalTime.MIDNIGHT) continue
            timed++
            when (at.hour) {
                in 5..11 -> parts[0]++
                in 12..16 -> parts[1]++
                in 17..21 -> parts[2]++
                else -> parts[3]++
            }
        }
        if (timed == 0) return weekday.toList() to null
        var dominant = 0
        for (i in 1..3) if (parts[i] > parts[dominant]) dominant = i
        val names = listOf("morning", "afternoon", "evening", "night")
        return weekday.toList() to StatsTimeOfDay(parts[0], parts[1], parts[2], parts[3], names[dominant])
    }

    /**
     * The longest run of consecutive local days with a play, and the current one: the run ending
     * on the latest play day, counted only if that day is today or yesterday. A past year has no
     * current streak at all.
     */
    private fun streaks(plays: List<StatsPlay>): Pair<Int, Int?> {
        val days = plays.mapNotNull { localDay(it.watchedAt) }.distinct().sorted()
        var run = 0
        var longest = 0
        days.forEachIndexed { i, day ->
            run = if (i > 0 && ChronoUnit.DAYS.between(days[i - 1], day) == 1L) run + 1 else 1
            if (run > longest) longest = run
        }
        if (past) return longest to null
        val last = days.lastOrNull()
        val current = if (last != null && (last == today || last == today.minusDays(1))) run else 0
        return longest to current
    }

    /** The period's oldest play; an episode refers to its show and carries its position. */
    private fun firstPlay(plays: List<StatsPlay>): StatsFirstPlay? {
        for (r in plays) {
            if (r.targetType == TARGET_EPISODE_TYPE) {
                val title = r.showId?.let(::show) ?: continue
                val episode = if (r.season != null && r.number != null) StatsEpisodeRef(r.season, r.number) else null
                return StatsFirstPlay(title, episode, r.watchedAt)
            }
            val title = movie(r.targetId) ?: continue
            return StatsFirstPlay(title, null, r.watchedAt)
        }
        return null
    }

    /**
     * Distinct watched titles by release decade, contiguous from the oldest to the newest so the
     * chart has no gaps. The favourite is the fullest decade, a tie going to the more recent;
     * oldest and newest are the extreme years, a tie going to more plays, then the more recent
     * play, then kind and id.
     */
    fun decades(titles: List<TitleAgg>): StatsDecades? {
        val dated = titles.filter { it.year != null }
        if (dated.isEmpty()) return null
        val counts = dated.groupingBy { it.year!! / 10 * 10 }.eachCount()
        val minDec = counts.keys.min()
        val maxDec = counts.keys.max()
        val buckets = ArrayList<StatsDecade>()
        var favorite = 0
        for (dec in minDec..maxDec step 10) {
            buckets += StatsDecade(dec, counts[dec] ?: 0)
            if ((counts[dec] ?: 0) >= (counts[favorite] ?: 0)) favorite = dec
        }
        fun prefer(a: TitleAgg, b: TitleAgg): Boolean = when {
            a.plays != b.plays -> a.plays > b.plays
            a.lastAt != b.lastAt -> a.lastAt > b.lastAt
            else -> byKey.compare(a.key, b.key) < 0
        }
        var oldest = dated.first()
        var newest = dated.first()
        for (t in dated.drop(1)) {
            if (t.year!! < oldest.year!! || (t.year == oldest.year && prefer(t, oldest))) oldest = t
            if (t.year > newest.year!! || (t.year == newest.year && prefer(t, newest))) newest = t
        }
        fun era(t: TitleAgg): StatsEraTitle? = title(t.key)?.let { StatsEraTitle(it, t.year!!) }
        return StatsDecades(
            buckets = buckets,
            favorite = favorite,
            oldest = era(oldest),
            newest = if (dated.size > 1) era(newest) else null,
            covered = dated.size,
            total = titles.size,
        )
    }

    /**
     * Titles played more than once — a show's count being the sum of its episode plays — most
     * plays first, then the most recent play, then kind and id, at most five.
     */
    fun mostRewatched(titles: List<TitleAgg>): List<StatsRewatched> =
        titles.filter { it.plays > 1 }
            .sortedWith(
                compareByDescending<TitleAgg> { it.plays }.thenByDescending { it.lastAt }
                    .thenComparing({ it.key }, byKey),
            )
            .asSequence()
            .mapNotNull { t -> title(t.key)?.let { StatsRewatched(it, t.plays, t.lastAt) } }
            .take(REWATCHED_LIMIT)
            .toList()

    /**
     * Every followed, non-dropped show: how many are finished, the mean completion over shows with
     * an aired episode, and the three closest to done — fewest episodes left, then the higher
     * ratio, then the lowest id. All time only: the state table holds current facts without dates.
     */
    fun completion(rows: List<StatsCompletionRow>): StatsCompletion? {
        if (rows.isEmpty()) return null
        val completed = rows.count { it.aired > 0 && it.watched >= it.aired }
        val aired = rows.filter { it.aired > 0 }
        fun ratio(r: StatsCompletionRow) = r.watched.toDouble() / r.aired
        val average = if (aired.isEmpty()) 0.0 else aired.sumOf { minOf(1.0, ratio(it)) } / aired.size
        val almostThere = aired.filter { it.watched > 0 && it.watched < it.aired }
            .sortedWith(
                compareBy<StatsCompletionRow> { it.aired - it.watched }.thenByDescending { ratio(it) }
                    .thenBy { it.showId },
            )
            .asSequence()
            .mapNotNull { r -> show(r.showId)?.let { StatsAlmostThere(it, r.watched, r.aired, r.aired - r.watched) } }
            .take(ALMOST_THERE_LIMIT)
            .toList()
        return StatsCompletion(rows.size, completed, rows.size - completed, average, almostThere)
    }
}
