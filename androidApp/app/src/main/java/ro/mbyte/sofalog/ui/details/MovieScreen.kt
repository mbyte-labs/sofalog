package ro.mbyte.sofalog.ui.details

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.coerceAtLeast
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.data.CopyKind
import ro.mbyte.sofalog.data.MovieUserState
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.db.MovieEntity
import ro.mbyte.sofalog.data.db.WatchEntity
import ro.mbyte.sofalog.domain.MovieDetails
import ro.mbyte.sofalog.domain.parseMovieDetails
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.WatchLogForm
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.formatWatchInstant
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.userMessage
import ro.mbyte.sofalog.ui.details.parts.CastSection
import ro.mbyte.sofalog.ui.details.parts.Byline
import ro.mbyte.sofalog.ui.details.parts.CopyTileState
import ro.mbyte.sofalog.ui.details.parts.DetailFact
import ro.mbyte.sofalog.ui.details.parts.DetailHero
import ro.mbyte.sofalog.ui.details.parts.DetailsSection
import ro.mbyte.sofalog.ui.details.parts.TimelineEntry
import ro.mbyte.sofalog.ui.details.parts.compactUsd
import ro.mbyte.sofalog.ui.details.parts.formatRuntime
import ro.mbyte.sofalog.ui.details.parts.languageName
import ro.mbyte.sofalog.ui.details.parts.popularityFact
import ro.mbyte.sofalog.ui.details.parts.HistorySection
import ro.mbyte.sofalog.ui.details.parts.Lightbox
import ro.mbyte.sofalog.ui.details.parts.MediaSection
import ro.mbyte.sofalog.ui.details.parts.OverlaidTopBar
import ro.mbyte.sofalog.ui.details.parts.StatusBadges
import ro.mbyte.sofalog.ui.details.parts.rememberHeroCollapse
import ro.mbyte.sofalog.ui.details.parts.RatingsSection
import ro.mbyte.sofalog.ui.details.parts.SimilarSection
import ro.mbyte.sofalog.ui.details.parts.WatchNowSection
import java.time.Instant

data class MovieUiState(
    val movie: MovieEntity? = null,
    /** Everything the page reads out of the stored body beyond the row's own columns. */
    val details: MovieDetails = MovieDetails.EMPTY,
    val watches: List<WatchEntity> = emptyList(),
    val user: MovieUserState = MovieUserState(),
    val loadError: String? = null,
    val busy: Boolean = false,
    val message: String? = null,
)

/** What the view model keeps beside the database's own flows. */
private data class MovieExtras(
    val loadError: String? = null,
    val busy: Boolean = false,
    val message: String? = null,
    /** The state a write in flight is expected to produce, shown until the database says so. */
    val optimistic: MovieUserState? = null,
)

class MovieViewModel(private val repo: VideoRepository, private val tmdbId: Long) : ViewModel() {
    /** The live, unsaved view of a movie the library does not hold. */
    private val preview = MutableStateFlow<MovieEntity?>(null)
    private val extras = MutableStateFlow(MovieExtras())

    /** Rating changes, latest wins: sliding along the bar asks for many and only the last matters. */
    private val ratings = Channel<Int?>(Channel.CONFLATED)
    private var ratingsAsked = 0

    private val movie = combine(repo.observeMovie(tmdbId), preview) { local, live -> local ?: live }
        .distinctUntilChanged()
        .map { it to (it?.let { m -> parseMovieDetails(m.metadataJson) } ?: MovieDetails.EMPTY) }
        .flowOn(Dispatchers.Default)

    val state: StateFlow<MovieUiState> = combine(
        movie, repo.observeMovieWatches(tmdbId), repo.observeMovieUserState(tmdbId), extras,
    ) { (row, details), watches, user, extra ->
        MovieUiState(row, details, watches, extra.optimistic ?: user, extra.loadError, extra.busy, extra.message)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), MovieUiState())

    init {
        viewModelScope.launch {
            if (repo.observeMovie(tmdbId).first() == null) {
                attempt { repo.previewMovie(tmdbId) }
                    .onSuccess { preview.value = it }
                    .onFailure { e -> extras.value = extras.value.copy(loadError = e.userMessage()) }
            } else {
                // A stale snapshot refreshes on view; when TMDB is down the stored one keeps rendering.
                attempt { repo.refreshMovieIfStale(tmdbId) }
            }
        }
        viewModelScope.launch {
            for (rating in ratings) {
                val asked = ratingsAsked
                val failure = attempt { repo.rateMovie(tmdbId, rating) }.exceptionOrNull()
                // A newer request is already queued: its optimistic value stays up.
                if (asked == ratingsAsked) {
                    extras.value = extras.value.copy(optimistic = null, message = failure?.userMessage() ?: extras.value.message)
                }
            }
        }
    }

    fun rate(rating: Int?) {
        ratingsAsked++
        extras.value = extras.value.copy(optimistic = state.value.user.copy(rating = rating))
        ratings.trySend(rating)
    }

    fun logWatch(watchedAt: Instant?, rating: Int?) = act { repo.markMovieWatched(tmdbId, watchedAt, rating) }

    fun deleteWatch(id: Long) = act { repo.deleteWatch(id) }

    fun setWatchlisted(on: Boolean) =
        act(state.value.user.copy(watchlisted = on)) { repo.setMovieWatchlisted(tmdbId, on) }

    fun setFavorited(on: Boolean) =
        act(state.value.user.copy(favorited = on)) { repo.setMovieFavorited(tmdbId, on) }

    fun setCopy(kind: CopyKind, owned: Boolean) = act { repo.setMovieCopy(tmdbId, kind, owned) }

    fun messageShown() {
        extras.value = extras.value.copy(message = null)
    }

    /** One write at a time; [optimistic] is shown while it runs and dropped when it lands or fails. */
    private fun act(optimistic: MovieUserState? = null, block: suspend () -> Unit) {
        if (extras.value.busy) return
        extras.value = extras.value.copy(busy = true, optimistic = optimistic)
        viewModelScope.launch {
            val failure = attempt { block() }.exceptionOrNull()
            extras.value = extras.value.copy(busy = false, optimistic = null, message = failure?.userMessage())
        }
    }
}

/** Which sheet or dialog the page has open — one at a time. */
private sealed interface MovieOverlay {
    data object LogWatch : MovieOverlay
    data class Copy(val kind: CopyKind, val owned: Boolean) : MovieOverlay
    data class RemoveWatch(val watch: WatchEntity) : MovieOverlay
    data class Images(val urls: List<String>, val start: Int) : MovieOverlay
}

/**
 * The movie page — the web page's sections in the web page's order, laid out for touch: every
 * choice is a bottom sheet, the rails scroll edge to edge, and the gallery is a swipeable viewer.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MovieScreen(tmdbId: Long, onMenu: () -> Unit, onOpenMovie: (Long) -> Unit) {
    val repo = repository()
    val vm: MovieViewModel = viewModel(factory = viewModelFactory { initializer { MovieViewModel(repo, tmdbId) } })
    val state by vm.state.collectAsStateWithLifecycle()
    var overlay by remember { mutableStateOf<MovieOverlay?>(null) }
    val movie = state.movie

    val snackbar = remember { SnackbarHostState() }
    val list = rememberLazyListState()
    LaunchedEffect(state.message) {
        state.message?.let { snackbar.showSnackbar(it); vm.messageShown() }
    }

    Scaffold(snackbarHost = { SnackbarHost(snackbar) }, contentWindowInsets = WindowInsets(0, 0, 0, 0)) { padding ->
        BoxWithConstraints(Modifier.fillMaxSize().padding(padding)) {
            val wide = maxWidth >= 840.dp
            val page = if (wide) 32.dp else 16.dp
            // The hero's backdrop: 16:9 across the window, but never most of a short one.
            val bandHeight = minOf(maxWidth * 9 / 16, maxHeight * 0.45f).coerceAtLeast(180.dp)
            when {
                movie == null && state.loadError != null -> CenteredMessage(state.loadError!!)
                movie == null -> CenteredLoading()
                else -> LazyColumn(
                    Modifier.fillMaxSize(),
                    state = list,
                    contentPadding = PaddingValues(
                        bottom = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding() + 32.dp,
                    ),
                ) {
                    val enabled = !state.busy
                    val details = state.details
                    item(key = "hero") {
                        DetailHero(
                            title = movie.title,
                            posterPath = movie.posterPath,
                            backdropPath = movie.backdropPath,
                            facts = listOfNotNull(
                                movie.releaseDate?.take(4),
                                movie.runtime?.takeIf { it > 0 }?.let(::formatRuntime),
                            ),
                            byline = details.director?.let { Byline("Directed by", it) },
                            genres = movie.genres,
                            tagline = details.tagline,
                            overview = movie.overview,
                            noOverview = "TMDB has no overview for this movie.",
                            wide = wide,
                            page = page,
                            bandHeight = bandHeight,
                        )
                    }
                    item(key = "details") {
                        DetailsSection(
                            tmdbUrl = "https://www.themoviedb.org/movie/${movie.tmdbId}",
                            imdbId = details.imdbId,
                            homepage = details.homepage,
                            trailerUrl = details.trailerUrl,
                            facts = listOfNotNull(
                                details.originalLanguage?.let { DetailFact("Language", languageName(it)) },
                                details.budget?.let { DetailFact("Budget", compactUsd(it)) },
                                details.revenue?.let { DetailFact("Revenue", compactUsd(it)) },
                                popularityFact(details.popularity, movie.fetchedAt),
                            ),
                            wide = wide,
                            page = page,
                        )
                    }
                    item(key = "ratings") {
                        RatingsSection(movie.voteAverage, details.votes, state.user.rating, vm::rate, wide, page)
                    }
                    item(key = "watch-now") {
                        WatchNowSection(
                            tiles = CopyKind.entries.map { CopyTileState(it, owned = it in state.user.copies) },
                            streaming = state.details.streaming,
                            subject = "movie",
                            enabled = enabled,
                            onCopyTap = { overlay = MovieOverlay.Copy(it, it in state.user.copies) },
                            wide = wide,
                            page = page,
                        )
                    }
                    item(key = "history") {
                        HistorySection(
                            entries = state.watches.map { TimelineEntry(it.id, it.watchedAt) },
                            watched = state.watches.isNotEmpty(),
                            watchlisted = state.user.watchlisted,
                            favorited = state.user.favorited,
                            enabled = enabled,
                            onEntryTap = { entry ->
                                state.watches.firstOrNull { it.id == entry.id }?.let { overlay = MovieOverlay.RemoveWatch(it) }
                            },
                            onLogWatch = { overlay = MovieOverlay.LogWatch },
                            onWatchlist = vm::setWatchlisted,
                            onFavorite = vm::setFavorited,
                            wide = wide,
                            page = page,
                        )
                    }
                    item(key = "cast") {
                        CastSection(movie.title, details.cast, "TMDB lists no cast for this movie.", wide, page)
                    }
                    item(key = "media") {
                        MediaSection(
                            details.videos, details.backdrops, details.posters, "TMDB lists no media for this movie.", wide, page,
                        ) { urls, start -> overlay = MovieOverlay.Images(urls, start) }
                    }
                    item(key = "similar") {
                        SimilarSection("Similar movies", "TMDB lists no similar movies.", details.similar, wide, page, onOpenMovie)
                    }
                }
            }
            val collapsed by rememberHeroCollapse(list, loaded = movie != null, bandHeight = bandHeight)
            OverlaidTopBar(movie?.title ?: "Movie", collapsed, wide, onMenu) {
                if (movie != null) {
                    StatusBadges(
                        watched = state.watches.isNotEmpty(),
                        watchlisted = state.user.watchlisted,
                        favorited = state.user.favorited,
                    )
                }
            }
        }
    }

    val close = { overlay = null }
    when (val open = overlay) {
        null -> Unit
        is MovieOverlay.LogWatch -> if (movie != null) {
            ModalBottomSheet(onDismissRequest = close, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
                WatchLogForm(
                    title = if (state.watches.isEmpty()) "Log a watch" else "Log another watch",
                    subtitle = movie.title,
                    airDate = movie.releaseDate,
                    onConfirm = { watchedAt, rating ->
                        vm.logWatch(watchedAt, rating)
                        close()
                    },
                    modifier = Modifier.padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
                    subject = "movie",
                    initialRating = state.user.rating,
                )
            }
        }
        is MovieOverlay.Copy -> ModalBottomSheet(onDismissRequest = close) {
            val verb = if (open.owned) "Remove" else "Add"
            Column(
                Modifier.fillMaxWidth().padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("$verb ${open.kind.label} copy?", style = MaterialTheme.typography.titleLarge, textAlign = TextAlign.Center)
                Text(movie?.title.orEmpty(), style = MaterialTheme.typography.titleSmall, textAlign = TextAlign.Center)
                Button(
                    onClick = {
                        vm.setCopy(open.kind, !open.owned)
                        close()
                    },
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp).height(52.dp),
                ) { Text("$verb ${open.kind.label} copy") }
                TextButton(onClick = close, modifier = Modifier.fillMaxWidth().height(48.dp)) { Text("Cancel") }
            }
        }
        is MovieOverlay.RemoveWatch -> ModalBottomSheet(onDismissRequest = close) {
            Column(
                Modifier.fillMaxWidth().padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("Remove this watch?", style = MaterialTheme.typography.titleLarge, textAlign = TextAlign.Center)
                Text(movie?.title.orEmpty(), style = MaterialTheme.typography.titleSmall, textAlign = TextAlign.Center)
                Text(
                    "Watched ${formatWatchInstant(open.watch.watchedAt)}. This cannot be undone.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
                Button(
                    onClick = {
                        vm.deleteWatch(open.watch.id)
                        close()
                    },
                    colors = ButtonDefaults.buttonColors(
                        containerColor = MaterialTheme.colorScheme.error,
                        contentColor = MaterialTheme.colorScheme.onError,
                    ),
                    modifier = Modifier.fillMaxWidth().padding(top = 12.dp).height(52.dp),
                ) { Text("Remove this watch") }
                TextButton(onClick = close, modifier = Modifier.fillMaxWidth().height(48.dp)) { Text("Cancel") }
            }
        }
        is MovieOverlay.Images -> Lightbox(open.urls, open.start, onDismiss = close)
    }
}
