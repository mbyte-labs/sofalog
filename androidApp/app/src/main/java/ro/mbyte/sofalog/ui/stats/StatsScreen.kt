package ro.mbyte.sofalog.ui.stats

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Button
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.max
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.domain.DAY_PARTS
import ro.mbyte.sofalog.domain.STATS_MOVIE
import ro.mbyte.sofalog.domain.STATS_PERIOD_ALL
import ro.mbyte.sofalog.domain.Stats
import ro.mbyte.sofalog.domain.StatsActivity
import ro.mbyte.sofalog.domain.StatsCompletion
import ro.mbyte.sofalog.domain.StatsCrowd
import ro.mbyte.sofalog.domain.StatsCrowdEntry
import ro.mbyte.sofalog.domain.StatsDecades
import ro.mbyte.sofalog.domain.StatsGenres
import ro.mbyte.sofalog.domain.StatsHeadline
import ro.mbyte.sofalog.domain.StatsRatingSample
import ro.mbyte.sofalog.domain.StatsRatingTrend
import ro.mbyte.sofalog.domain.StatsRatings
import ro.mbyte.sofalog.domain.StatsTitle
import ro.mbyte.sofalog.domain.StatsUnratedMovies
import ro.mbyte.sofalog.domain.UserZone
import ro.mbyte.sofalog.domain.WEEKDAYS_LONG
import ro.mbyte.sofalog.domain.WEEKDAYS_SHORT
import ro.mbyte.sofalog.domain.compactNumber
import ro.mbyte.sofalog.domain.counts
import ro.mbyte.sofalog.domain.crowdVerdict
import ro.mbyte.sofalog.domain.decadeLabel
import ro.mbyte.sofalog.domain.deltaSign
import ro.mbyte.sofalog.domain.isEmpty
import ro.mbyte.sofalog.domain.oneDecimal
import ro.mbyte.sofalog.domain.percent
import ro.mbyte.sofalog.domain.plural
import ro.mbyte.sofalog.domain.signedDelta
import ro.mbyte.sofalog.domain.splitPercent
import ro.mbyte.sofalog.domain.trendVerdict
import ro.mbyte.sofalog.domain.watchDay
import ro.mbyte.sofalog.domain.watchedAgo
import ro.mbyte.sofalog.ui.common.AppLogo
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.formatWatchDay
import ro.mbyte.sofalog.ui.common.ratingColor
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.details.DetailScaffold
import ro.mbyte.sofalog.ui.details.parts.SubLabel
import java.time.LocalDate
import kotlin.math.roundToInt

/** What the page shows: nothing yet, the numbers, or that they could not be computed. */
sealed interface StatsState {
    data object Loading : StatsState
    data object Failed : StatsState
    data class Ready(val stats: Stats) : StatsState
}

/** The rate sheet's state: which movie, whether its write is running, and how it failed. */
data class RateTarget(val title: StatsTitle, val busy: Boolean = false, val error: String? = null)

@OptIn(ExperimentalCoroutinesApi::class)
class StatsViewModel(private val repo: VideoRepository) : ViewModel() {
    /** null asks for the default: the current year when it holds a play, else all time. */
    val period = MutableStateFlow<String?>(null)
    private val attempts = MutableStateFlow(0)

    /**
     * Live: a watch or a rating anywhere re-emits, so there is nothing to refetch after rating a
     * movie here. Switching period keeps the old numbers on screen until the new ones arrive.
     */
    val state: StateFlow<StatsState> = combine(period, attempts) { p, _ -> p }
        .flatMapLatest { p ->
            repo.observeStats(p)
                .map<Stats, StatsState> { StatsState.Ready(it) }
                .catch { emit(StatsState.Failed) }
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), StatsState.Loading)

    val rating = MutableStateFlow<RateTarget?>(null)

    fun retry() = attempts.update { it + 1 }

    fun openRate(title: StatsTitle) {
        rating.value = RateTarget(title)
    }

    fun closeRate() {
        if (rating.value?.busy != true) rating.value = null
    }

    fun rate(value: Int) {
        val target = rating.value ?: return
        if (target.busy) return
        rating.value = target.copy(busy = true, error = null)
        viewModelScope.launch {
            attempt { repo.rateMovie(target.title.tmdbId, value) }
                .onSuccess { rating.value = null }
                .onFailure { rating.value = target.copy(busy = false, error = "Could not save your rating. Please try again.") }
        }
    }
}

/**
 * The Stats page (SPEC D-039): the user's own numbers, computed on the device from its own
 * database — never a provider call. One column on a phone; on a tablet the cards pair up.
 */
@Composable
fun StatsScreen(
    onMenu: () -> Unit,
    onOpenMovie: (Long) -> Unit,
    onOpenShow: (Long) -> Unit,
    onDiscover: () -> Unit,
) {
    val repo = repository()
    val vm: StatsViewModel = viewModel(factory = viewModelFactory { initializer { StatsViewModel(repo) } })
    val state by vm.state.collectAsStateWithLifecycle()
    val asked by vm.period.collectAsStateWithLifecycle()
    val rating by vm.rating.collectAsStateWithLifecycle()
    val open: (StatsTitle) -> Unit = { if (it.kind == STATS_MOVIE) onOpenMovie(it.tmdbId) else onOpenShow(it.tmdbId) }

    DetailScaffold("Stats", onMenu, message = null, onMessageShown = {}) { modifier ->
        when (val current = state) {
            StatsState.Loading -> CenteredLoading(modifier)
            StatsState.Failed -> CenteredMessage("Something went wrong while computing your stats.", modifier) {
                TextButton(onClick = vm::retry) { Text("Retry") }
            }
            is StatsState.Ready -> StatsContent(
                stats = current.stats,
                selected = asked ?: current.stats.period,
                onPeriod = { vm.period.value = it },
                onOpen = open,
                onRate = vm::openRate,
                onDiscover = onDiscover,
                modifier = modifier,
            )
        }
    }
    rating?.let { target ->
        RateSheet(target.title, target.busy, target.error, onSave = vm::rate, onDismiss = vm::closeRate)
    }
}

/** The page's width classes — the detail pages' breakpoints. */
private class Layout(val width: Dp) {
    /** Tablet: cards pair up. */
    val wide = width >= 840.dp

    /** Beyond a phone: the four headline tiles fit one row. */
    val medium = width >= 600.dp
}

@Composable
private fun StatsContent(
    stats: Stats,
    selected: String,
    onPeriod: (String) -> Unit,
    onOpen: (StatsTitle) -> Unit,
    onRate: (StatsTitle) -> Unit,
    onDiscover: () -> Unit,
    modifier: Modifier,
) {
    BoxWithConstraints(modifier) {
        val layout = Layout(maxWidth)
        // The gutter of the detail pages, growing so the content never runs wider than it reads well.
        val side = max(if (layout.wide) 32.dp else 16.dp, (maxWidth - 1100.dp) / 2)
        val today = remember(stats.today) { LocalDate.parse(stats.today) }
        val isYear = stats.period != STATS_PERIOD_ALL
        val periodLabel = if (isYear) stats.period else "All time"

        LazyColumn(contentPadding = PaddingValues(start = side, end = side, top = 8.dp, bottom = 32.dp)) {
            if (stats.years.isNotEmpty()) {
                item("period") { PeriodChips(stats.years, selected, onPeriod) }
            }
            if (stats.isEmpty()) {
                item("empty") { EmptyStats(isYear, stats.period, onDiscover) }
                return@LazyColumn
            }
            item("headline") {
                StatsSection(periodLabel) { HeadlineBlock(stats.headline, layout) }
            }
            stats.mostWatchedShow?.let { mw ->
                item("most-watched") {
                    val hours = mw.minutes / 60
                    val last = watchDay(mw.lastPlayedAt, UserZone.current) ?: today
                    StatsSection("Most watched show") {
                        StatsBanner(
                            title = mw.show,
                            badge = if (hours > 0) "Most watched · ${plural(hours, "hour", "hours")}" else "Most watched",
                            subtitle = "${plural(mw.plays, "episode play", "episode plays")} · watched ${watchedAgo(last, today)}",
                            onOpen = { onOpen(mw.show) },
                        )
                    }
                }
            }
            ratingSections(stats, layout, isYear && stats.period.toInt() < today.year, onOpen)
            stats.crowd?.let { crowd ->
                item("crowd") { StatsSection("You vs the crowd") { CrowdCard(crowd, layout.wide, onOpen) } }
            }
            stats.unratedMovies?.let { unrated ->
                item("unrated") { UnratedBlock(unrated, today, onOpen, onRate) }
            }
            item("activity") { ActivityBlock(stats.activity, stats.period, isYear, today, layout, onOpen) }
            stats.decades?.let { decades ->
                item("decades") { DecadesBlock(decades, layout, onOpen) }
            }
            if (stats.mostRewatched.isNotEmpty()) {
                item("rewatched") {
                    StatsSection("Most rewatched") {
                        StatCard {
                            stats.mostRewatched.forEachIndexed { i, r ->
                                StatsTitleRow(
                                    title = r.title,
                                    onOpen = { onOpen(r.title) },
                                    rank = i + 1,
                                    subtitle = r.title.year?.toString(),
                                    figure = "${r.plays}×",
                                )
                            }
                        }
                    }
                }
            }
            val completion = stats.completion
            val genres = stats.genres
            when {
                completion != null && genres != null && layout.wide -> item("completion-genres") {
                    TwoUp(
                        true,
                        { StatsSection("Show completion") { CompletionCard(completion, onOpen) } },
                        { StatsSection("Top genres") { GenresCard(genres, stats.period) } },
                    )
                }
                else -> {
                    completion?.let { item("completion") { StatsSection("Show completion") { CompletionCard(it, onOpen) } } }
                    genres?.let { item("genres") { StatsSection("Top genres") { GenresCard(it, stats.period) } } }
                }
            }
        }
    }
}

/** "All time", then every year the log or the ratings reach, newest first. Picking one applies it. */
@Composable
private fun PeriodChips(years: List<Int>, selected: String, onPeriod: (String) -> Unit) {
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        (listOf(STATS_PERIOD_ALL) + years.map { it.toString() }).forEach { period ->
            FilterChip(
                selected = period == selected,
                onClick = { onPeriod(period) },
                label = { Text(if (period == STATS_PERIOD_ALL) "All time" else period) },
            )
        }
    }
}

@Composable
private fun EmptyStats(isYear: Boolean, period: String, onDiscover: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().padding(vertical = 64.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        AppLogo(size = 56.dp, corner = 14.dp)
        Spacer(Modifier.height(8.dp))
        Text(
            if (isYear) "Nothing watched or rated in $period" else "There are no stats to show yet",
            style = MaterialTheme.typography.titleMedium,
            textAlign = TextAlign.Center,
        )
        Text(
            if (isYear) "Pick another year, or go back to All time." else "Watch something and your stats will appear here.",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(8.dp))
        FilledTonalButton(onClick = onDiscover) { Text("Discover") }
    }
}

/** The four headline numbers and how the time splits between TV and movies. */
@Composable
private fun HeadlineBlock(headline: StatsHeadline, layout: Layout) {
    val tiles = listOf(
        compactNumber(headline.hours) to "Hours watched",
        compactNumber(headline.plays) to "Total plays",
        compactNumber(headline.movies) to "Movies",
        compactNumber(headline.episodes) to "Episodes",
    )
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        tiles.chunked(if (layout.medium) 4 else 2).forEach { row ->
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                row.forEach { (value, label) -> StatTile(value, label, Modifier.weight(1f)) }
            }
        }
        val total = headline.movieMinutes + headline.episodeMinutes
        if (total > 0) {
            val tv = MaterialTheme.colorScheme.primary
            val movies = MaterialTheme.colorScheme.tertiary
            StatCard {
                CardTitle(
                    if (headline.episodeMinutes * 2 >= total) {
                        "${splitPercent(headline.episodeMinutes, total)} of your time is TV"
                    } else {
                        "${splitPercent(headline.movieMinutes, total)} of your time is movies"
                    },
                )
                Row(Modifier.fillMaxWidth().height(10.dp).clip(CircleShape)) {
                    if (headline.episodeMinutes > 0) Box(Modifier.weight(headline.episodeMinutes.toFloat()).height(10.dp).background(tv))
                    if (headline.movieMinutes > 0) Box(Modifier.weight(headline.movieMinutes.toFloat()).height(10.dp).background(movies))
                }
                Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                    Legend(tv, "TV · ${compactNumber(headline.episodeMinutes / 60)}h")
                    Legend(movies, "Movies · ${compactNumber(headline.movieMinutes / 60)}h")
                }
            }
        }
    }
}

@Composable
private fun Legend(color: Color, text: String) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Box(Modifier.size(10.dp).clip(CircleShape).background(color))
        Caption(text)
    }
}

/**
 * Your ratings, your highest rated, ratings over time — in that order on a phone. On a tablet the
 * two charts sit side by side and the rail follows them.
 */
private fun LazyListScope.ratingSections(stats: Stats, layout: Layout, pastYear: Boolean, onOpen: (StatsTitle) -> Unit) {
    val ratings = stats.ratings
    val trend = stats.ratingTrend
    val rail: LazyListScope.() -> Unit = {
        if (stats.topRated.isNotEmpty()) {
            item("top-rated") {
                StatsSection("Your highest rated") {
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        items(stats.topRated, key = { "${it.title.kind}-${it.title.id}" }) { top ->
                            StatsPosterTile(top.title, top.rating, if (layout.wide) 132.dp else 108.dp) { onOpen(top.title) }
                        }
                    }
                }
            }
        }
    }
    if (layout.wide && ratings != null && trend != null) {
        item("ratings-trend") {
            TwoUp(
                true,
                { StatsSection("Your ratings") { RatingsCard(ratings) } },
                {
                    StatsSection("Ratings over time", "Average rating per month (movies & shows)") {
                        TrendCard(trend, stats.period, pastYear)
                    }
                },
            )
        }
        rail()
        return
    }
    ratings?.let { item("ratings") { StatsSection("Your ratings") { RatingsCard(it) } } }
    rail()
    trend?.let {
        item("trend") {
            StatsSection("Ratings over time", "Average rating per month (movies & shows)") { TrendCard(it, stats.period, pastYear) }
        }
    }
}

@Composable
private fun RatingsCard(ratings: StatsRatings) {
    StatCard {
        StatsBarChart(
            values = ratings.distribution.map { it.toFloat() },
            labels = (1..10).map { it.toString() },
            description = "Ratings given, 1 to 10",
            detail = { i -> "${ratings.distribution[i]} rated ${i + 1}" },
            colors = (1..10).map { ratingColor(it) },
        )
        Facts(
            listOfNotNull(
                "Ratings given" to ratings.count.toString(),
                "Average rating" to oneDecimal(ratings.average),
                ratings.moviesAverage?.let { "Movies average" to oneDecimal(it) },
                ratings.showsAverage?.let { "Shows average" to oneDecimal(it) },
                ratings.episodesAverage?.let { "Episodes average" to oneDecimal(it) },
            ),
        )
    }
}

@Composable
private fun TrendCard(trend: StatsRatingTrend, period: String, pastYear: Boolean) {
    fun sample(s: StatsRatingSample) = s.average?.let { "${oneDecimal(it)} avg (${s.count})" } ?: "— (0)"
    StatCard {
        StatsBarChart(
            values = trend.months.map { (it.average ?: 0.0).toFloat() },
            labels = trend.months.map { formatStatsMonth(it.month, short = true) },
            description = "Average rating per month",
            detail = { i ->
                val m = trend.months[i]
                val body = m.average?.let { "${oneDecimal(it)} avg · ${plural(m.count, "rating", "ratings")}" } ?: "No ratings"
                "${formatStatsMonth(m.month, short = false)} · $body"
            },
            fixedMax = 10f,
            thinLabels = true,
        )
        Facts(
            listOf(
                (if (pastYear) "Last 90 days of $period" else "Last 90 days") to sample(trend.recent),
                "Before that" to sample(trend.earlier),
            ),
        )
        val delta = trend.delta
        val verdict = trend.verdict
        if (delta != null && verdict != null) {
            Column {
                SubLabel(trendVerdict(verdict))
                Text(
                    signedDelta(delta),
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = deltaColor(deltaSign(delta)),
                )
            }
        }
    }
}

@Composable
private fun CrowdCard(crowd: StatsCrowd, wide: Boolean, onOpen: (StatsTitle) -> Unit) {
    StatCard {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(
                signedDelta(crowd.delta),
                style = MaterialTheme.typography.headlineMedium,
                fontWeight = FontWeight.Bold,
                color = deltaColor(deltaSign(crowd.delta)),
            )
            Text(crowdVerdict(crowd.verdict), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
        }
        Facts(
            listOf(
                "Your average" to oneDecimal(crowd.userAverage),
                "TMDB average" to oneDecimal(crowd.crowdAverage),
                "Compared" to "${crowd.compared} of ${crowd.totalRated} rated titles",
            ),
        )
        val more: (@Composable () -> Unit)? = crowd.lovedMore.takeIf { it.isNotEmpty() }?.let {
            { CrowdList("You loved, the crowd didn't", it, 1, onOpen) }
        }
        val less: (@Composable () -> Unit)? = crowd.lovedLess.takeIf { it.isNotEmpty() }?.let {
            { CrowdList("The crowd loved, you didn't", it, -1, onOpen) }
        }
        if (more != null || less != null) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        if (more != null && less != null) {
            TwoUp(wide, more, less)
        } else {
            more?.invoke()
            less?.invoke()
        }
    }
}

@Composable
private fun CrowdList(label: String, entries: List<StatsCrowdEntry>, sign: Int, onOpen: (StatsTitle) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        SubLabel(label)
        entries.forEach { e ->
            StatsTitleRow(
                title = e.title,
                onOpen = { onOpen(e.title) },
                subtitle = listOfNotNull(e.title.year?.toString(), "You ${e.userRating}", "TMDB ${oneDecimal(e.tmdbAverage)}")
                    .joinToString(" · "),
                figure = signedDelta(e.gap),
                figureColor = deltaColor(sign),
            )
        }
    }
}

/** The most recently watched unrated movie as a banner, the next few as rows; each can be rated here. */
@Composable
private fun UnratedBlock(
    unrated: StatsUnratedMovies,
    today: LocalDate,
    onOpen: (StatsTitle) -> Unit,
    onRate: (StatsTitle) -> Unit,
) {
    StatsSection(
        "Waiting for your rating",
        plural(unrated.total, "watched movie without a rating", "watched movies without a rating"),
    ) {
        unrated.recent.firstOrNull()?.let { hero ->
            val day = watchDay(hero.lastPlayedAt, UserZone.current) ?: today
            StatsBanner(
                title = hero.movie,
                badge = "Watched ${watchedAgo(day, today)}",
                subtitle = hero.movie.year?.toString(),
                onOpen = { onOpen(hero.movie) },
                action = { Button(onClick = { onRate(hero.movie) }) { Text("Rate") } },
            )
        }
        val rest = unrated.recent.drop(1)
        if (rest.isNotEmpty()) {
            StatCard {
                rest.forEach { m ->
                    StatsTitleRow(
                        title = m.movie,
                        onOpen = { onOpen(m.movie) },
                        subtitle = m.movie.year?.toString(),
                        action = { TextButton(onClick = { onRate(m.movie) }) { Text("Rate") } },
                    )
                }
            }
        }
    }
}

@Composable
private fun ActivityBlock(
    activity: StatsActivity,
    period: String,
    isYear: Boolean,
    today: LocalDate,
    layout: Layout,
    onOpen: (StatsTitle) -> Unit,
) {
    val pastYear = isYear && period.toInt() < today.year
    StatsSection("Watch activity") {
        StatCard {
            CardTitle(if (isYear) "Plays per month in $period" else "Plays over the last 12 months")
            StatsBarChart(
                values = activity.months.map { it.plays.toFloat() },
                labels = activity.months.map { formatStatsMonth(it.month, short = true) },
                description = "Plays per month",
                detail = { i -> "${formatStatsMonth(activity.months[i].month, short = false)} · ${plural(activity.months[i].plays, "play", "plays")}" },
                thinLabels = !layout.medium,
            )
        }
        StatCard {
            CardTitle(if (isYear) "Plays per day in $period" else "Plays over the last year")
            PlaysHeatmap(activity.heatmap)
        }
        val weekday: (@Composable () -> Unit)? = if (activity.weekday.any { it > 0 }) {
            {
                StatCard {
                    CardTitle("Plays by weekday")
                    StatsBarChart(
                        values = activity.weekday.map { it.toFloat() },
                        labels = WEEKDAYS_SHORT,
                        description = "Plays by weekday",
                        detail = { i -> "${WEEKDAYS_LONG[i]} · ${plural(activity.weekday[i], "play", "plays")}" },
                        height = 130.dp,
                    )
                }
            }
        } else {
            null
        }
        val timeOfDay: (@Composable () -> Unit)? = activity.timeOfDay?.let { tod ->
            {
                val counts = tod.counts()
                val total = counts.sum().coerceAtLeast(1)
                val dominant = DAY_PARTS.firstOrNull { it.key == tod.dominant } ?: DAY_PARTS.first()
                val share = (counts[DAY_PARTS.indexOf(dominant)] * 100.0 / total).roundToInt()
                StatCard {
                    CardTitle("Plays by time of day")
                    StatsBarChart(
                        values = counts.map { it.toFloat() },
                        labels = DAY_PARTS.map { it.label },
                        description = "Plays by time of day",
                        detail = { i ->
                            "${DAY_PARTS[i].label} (${DAY_PARTS[i].range}) · ${plural(counts[i], "play", "plays")} · ${(counts[i] * 100.0 / total).roundToInt()}%"
                        },
                        height = 130.dp,
                    )
                    Facts(listOf(dominant.persona to "$share% ${dominant.span}"))
                }
            }
        }
        if (weekday != null && timeOfDay != null) {
            TwoUp(layout.wide, weekday, timeOfDay)
        } else {
            weekday?.invoke()
            timeOfDay?.invoke()
        }
        StatCard {
            Facts(
                listOfNotNull(
                    "Longest streak" to plural(activity.longestStreak, "day", "days"),
                    activity.currentStreak?.takeIf { !pastYear }?.let { "Current streak" to plural(it, "day", "days") },
                    activity.trackingDays?.let { "Tracking for" to plural(it, "day", "days") },
                    activity.activeDays?.let { "Active days" to plural(it, "day", "days") },
                ),
            )
        }
        activity.firstPlay?.let { first ->
            StatCard {
                CardTitle(if (isYear) "First play of $period" else "Where it all started")
                val day = formatWatchDay(first.watchedAt)
                StatsTitleRow(
                    title = first.title,
                    onOpen = { onOpen(first.title) },
                    subtitle = first.episode?.let { "S${it.season}E${it.number} · $day" } ?: day,
                )
            }
        }
    }
}

@Composable
private fun DecadesBlock(decades: StatsDecades, layout: Layout, onOpen: (StatsTitle) -> Unit) {
    StatsSection("Decades", "Titles watched by release decade") {
        StatCard {
            val short = decades.buckets.size < 10
            StatsBarChart(
                values = decades.buckets.map { it.titles.toFloat() },
                labels = decades.buckets.map { decadeLabel(it.decade, short) },
                description = "Titles watched by release decade",
                detail = { i -> "${decades.buckets[i].decade}s · ${plural(decades.buckets[i].titles, "title", "titles")}" },
                thinLabels = decades.buckets.size > 8,
            )
            val favoriteTitles = decades.buckets.firstOrNull { it.decade == decades.favorite }?.titles ?: 0
            if (decades.covered > 0) {
                Facts(listOf("Favorite decade" to "${decades.favorite}s · ${percent(favoriteTitles.toDouble() / decades.covered)}"))
            }
            if (decades.covered < decades.total) {
                Caption("Based on ${decades.covered} of ${decades.total} watched titles")
            }
        }
        val oldest: (@Composable () -> Unit)? = decades.oldest?.let { era ->
            { StatsBanner(era.title, "Oldest · ${era.year}", onOpen = { onOpen(era.title) }) }
        }
        val newest: (@Composable () -> Unit)? = decades.newest?.let { era ->
            { StatsBanner(era.title, "Newest · ${era.year}", onOpen = { onOpen(era.title) }) }
        }
        if (oldest != null && newest != null) {
            TwoUp(layout.medium, oldest, newest)
        } else {
            oldest?.invoke()
            newest?.invoke()
        }
    }
}

@Composable
private fun CompletionCard(completion: StatsCompletion, onOpen: (StatsTitle) -> Unit) {
    StatCard {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Average completion", style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
            Text(percent(completion.averageCompletion), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
        }
        StatMeter(completion.averageCompletion.toFloat(), MaterialTheme.colorScheme.tertiary)
        Facts(
            listOf(
                "Shows tracked" to completion.tracked.toString(),
                "Completed" to completion.completed.toString(),
                "In progress" to completion.inProgress.toString(),
            ),
        )
        if (completion.almostThere.isNotEmpty()) {
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            SubLabel("Almost there")
            completion.almostThere.forEach { a ->
                StatsTitleRow(
                    title = a.show,
                    onOpen = { onOpen(a.show) },
                    subtitle = "${plural(a.left, "episode left", "episodes left")} · ${a.watched} of ${a.aired} watched",
                    meter = if (a.aired > 0) a.watched.toFloat() / a.aired else 0f,
                )
            }
        }
    }
}

@Composable
private fun GenresCard(genres: StatsGenres, period: String) {
    StatCard {
        if (genres.entries.isEmpty()) {
            Caption("No genres known for these titles yet.")
        }
        val top = genres.entries.firstOrNull()?.titles?.coerceAtLeast(1) ?: 1
        genres.entries.forEach { g ->
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    g.name,
                    style = MaterialTheme.typography.bodyLarge,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    modifier = Modifier.width(132.dp),
                )
                StatMeter(g.titles.toFloat() / top, MaterialTheme.colorScheme.primary, Modifier.weight(1f))
                Text(
                    g.titles.toString(),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.End,
                    modifier = Modifier.width(36.dp),
                )
            }
        }
        if (genres.covered < genres.total) {
            Caption(
                if (period == STATS_PERIOD_ALL) {
                    "Based on ${genres.covered} of ${genres.total} watched titles"
                } else {
                    "Based on ${genres.covered} of ${genres.total} titles watched in $period"
                },
            )
        }
    }
}
