package ro.mbyte.sofalog.ui.discover

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.offset
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.IntOffset
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.systemGestureExclusion
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Slider
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.data.DiscoverOptions
import ro.mbyte.sofalog.data.DiscoverPage
import ro.mbyte.sofalog.data.MediaKind
import ro.mbyte.sofalog.data.SearchHit
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.domain.DiscoverFilters
import ro.mbyte.sofalog.domain.GenreOption
import ro.mbyte.sofalog.domain.LanguageOption
import ro.mbyte.sofalog.domain.LibraryMarks
import ro.mbyte.sofalog.domain.discoverHasMore
import ro.mbyte.sofalog.domain.genreOptions
import ro.mbyte.sofalog.ui.common.CenteredLoading
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.SegmentedChoice
import ro.mbyte.sofalog.ui.common.TitleCard
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.userMessage
import kotlin.math.roundToInt

enum class DiscoverTab { TRENDING, POPULAR }

data class DiscoverState(
    val tab: DiscoverTab = DiscoverTab.TRENDING,
    /** The type filter of both lists: null is movies and shows together. */
    val kind: MediaKind? = null,
    /** The Popular list's filters. Trending takes none — TMDB offers none there. */
    val filters: DiscoverFilters = DiscoverFilters(),
    val loading: Boolean = true,
    val hits: List<SearchHit> = emptyList(),
    val page: Int = 0,
    val totalPages: Int = 0,
    val error: String? = null,
    val loadingMore: Boolean = false,
)

/** The filter panel's lists. Their outage is only the panel's: the grid still loads. */
data class DiscoverOptionsState(
    val options: DiscoverOptions? = null,
    val loading: Boolean = false,
    val failed: Boolean = false,
)

/**
 * The Discover page (SPEC D-034, the device's subset): always fetched live from TMDB and never
 * stored. Only the marks on the cards come from the device.
 */
class DiscoverViewModel(private val repo: VideoRepository) : ViewModel() {
    val state = MutableStateFlow(DiscoverState())
    val options = MutableStateFlow(DiscoverOptionsState())

    /** A failure that keeps the list on screen: said once, in a snackbar. */
    val message = MutableStateFlow<String?>(null)
    private var job: Job? = null

    /** The library's marks for the cards on screen — the Search grid's, read from the device. */
    @OptIn(ExperimentalCoroutinesApi::class)
    val marks: StateFlow<LibraryMarks> = state.map { it.hits }.distinctUntilChanged()
        .flatMapLatest { repo.observeLibraryMarks(it) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), LibraryMarks.EMPTY)

    init {
        load()
    }

    fun setTab(tab: DiscoverTab) {
        if (tab == state.value.tab) return
        state.update { it.copy(tab = tab) }
        if (tab == DiscoverTab.POPULAR) loadOptions()
        load()
    }

    fun setKind(kind: MediaKind?) {
        if (kind == state.value.kind) return
        // A genre the new kind does not have would silently match nothing: drop it.
        val known = options.value.options?.let { o ->
            genreOptions(kind?.let { it == MediaKind.MOVIE }, o.movieGenres, o.showGenres).map { it.id }.toSet()
        }
        state.update {
            it.copy(kind = kind, filters = if (known == null) it.filters else it.filters.copy(genres = it.filters.genres intersect known))
        }
        load()
    }

    fun setFilters(filters: DiscoverFilters) {
        if (filters == state.value.filters) return
        state.update { it.copy(filters = filters) }
        if (state.value.tab == DiscoverTab.POPULAR) load()
    }

    /** Back to both kinds and the default filters, in one reload. */
    fun clearFilters() {
        val cleared = state.value.copy(kind = null, filters = DiscoverFilters())
        if (cleared == state.value) return
        state.value = cleared
        load()
    }

    fun retry() = load()

    /** Any change starts over at page one; the request it replaces is cancelled, never raced. */
    private fun load() {
        job?.cancel()
        state.update { it.copy(loading = true, loadingMore = false, error = null) }
        val asked = state.value
        job = viewModelScope.launch {
            attempt { fetch(asked, 1) }
                .onSuccess { page ->
                    state.update { it.copy(loading = false, hits = page.hits, page = page.page, totalPages = page.totalPages) }
                }
                .onFailure { e ->
                    state.update { it.copy(loading = false, hits = emptyList(), page = 0, totalPages = 0, error = e.userMessage()) }
                }
        }
    }

    /** The next page, appended. A failure keeps what is shown and the button stays as the retry. */
    fun loadMore() {
        val asked = state.value
        if (asked.loading || asked.loadingMore || !discoverHasMore(asked.page, asked.totalPages)) return
        state.update { it.copy(loadingMore = true) }
        job = viewModelScope.launch {
            attempt { fetch(asked, asked.page + 1) }
                .onSuccess { page ->
                    // TMDB's lists shift between requests: a title already shown is not repeated.
                    val shown = asked.hits.map { it.kind to it.tmdbId }.toSet()
                    state.update {
                        it.copy(
                            loadingMore = false,
                            hits = asked.hits + page.hits.filter { hit -> (hit.kind to hit.tmdbId) !in shown },
                            page = page.page,
                            totalPages = page.totalPages,
                        )
                    }
                }
                .onFailure { e ->
                    state.update { it.copy(loadingMore = false) }
                    message.value = e.userMessage()
                }
        }
    }

    private suspend fun fetch(asked: DiscoverState, page: Int): DiscoverPage = when (asked.tab) {
        DiscoverTab.TRENDING -> repo.trending(asked.kind, page)
        DiscoverTab.POPULAR -> repo.popular(asked.kind, asked.filters, page)
    }

    fun loadOptions() {
        val current = options.value
        if (current.options != null || current.loading) return
        options.value = DiscoverOptionsState(loading = true)
        viewModelScope.launch {
            attempt { repo.discoverOptions() }
                .onSuccess { options.value = DiscoverOptionsState(options = it) }
                .onFailure { options.value = DiscoverOptionsState(failed = true) }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DiscoverScreen(onOpen: (SearchHit) -> Unit, onMenu: () -> Unit) {
    val repo = repository()
    val vm: DiscoverViewModel = viewModel(factory = viewModelFactory { initializer { DiscoverViewModel(repo) } })
    val state by vm.state.collectAsStateWithLifecycle()
    val options by vm.options.collectAsStateWithLifecycle()
    val marks by vm.marks.collectAsStateWithLifecycle()
    val message by vm.message.collectAsStateWithLifecycle()
    val snackbar = remember { SnackbarHostState() }
    var filtersOpen by rememberSaveable { mutableStateOf(false) }
    val popular = state.tab == DiscoverTab.POPULAR

    // How far the list tabs are pushed out of view, 0 down to minus their height.
    var tabsHeight by remember { mutableIntStateOf(0) }
    var tabsOffset by remember { mutableFloatStateOf(0f) }
    val tabsScroll = remember {
        object : NestedScrollConnection {
            override fun onPreScroll(available: Offset, source: NestedScrollSource): Offset {
                tabsOffset = (tabsOffset + available.y).coerceIn(-tabsHeight.toFloat(), 0f)
                return Offset.Zero // the tabs follow the scroll; they take none of it
            }
        }
    }
    // A reload starts a new list at its top, where the tabs belong.
    LaunchedEffect(state.loading) {
        if (state.loading) tabsOffset = 0f
    }

    LaunchedEffect(message) {
        message?.let { snackbar.showSnackbar(it); vm.message.value = null }
    }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text("Discover") },
                navigationIcon = { TextButton(onClick = onMenu) { Text("☰") } },
                actions = {
                    // Trending has the kind alone to filter by; Popular adds its own filters.
                    val count = (if (state.kind != null) 1 else 0) + (if (popular) state.filters.activeCount else 0)
                    TextButton(onClick = { if (popular) vm.loadOptions(); filtersOpen = true }) {
                        Text(if (count > 0) "Filters ($count)" else "Filters")
                    }
                },
            )
        },
    ) { padding ->
        // The list tabs sit over the top of the content and slide away with it: scrolling down
        // pushes them out, and any scroll back up — wherever in the list — brings them back.
        Box(Modifier.fillMaxSize().padding(padding).clipToBounds().nestedScroll(tabsScroll)) {
            val tabsSpace = with(LocalDensity.current) { tabsHeight.toDp() }
            when {
                state.loading -> CenteredLoading(Modifier.padding(top = tabsSpace))
                state.error != null -> CenteredMessage(state.error!!, Modifier.padding(top = tabsSpace)) {
                    TextButton(onClick = vm::retry) { Text("Retry") }
                }
                state.hits.isEmpty() -> CenteredMessage(
                    if (popular && (state.filters.activeCount > 0 || state.filters.voteMin != null)) {
                        "Nothing on TMDB matches these filters."
                    } else {
                        "TMDB lists nothing here right now."
                    },
                    Modifier.padding(top = tabsSpace),
                )
                // The Search and Lists pages' grid, so a poster is the same size everywhere.
                else -> LazyVerticalGrid(
                    columns = GridCells.Adaptive(minSize = 150.dp),
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = tabsSpace + 16.dp, bottom = 16.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                    verticalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    // Keyed by the pair: a TMDB id is unique only within its kind.
                    items(state.hits, key = { "${it.kind}-${it.tmdbId}" }) { hit ->
                        TitleCard(hit, marks.of(hit.kind == MediaKind.MOVIE, hit.tmdbId)) { onOpen(hit) }
                    }
                    if (discoverHasMore(state.page, state.totalPages)) {
                        item(key = "more", span = { GridItemSpan(maxLineSpan) }) {
                            Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                                if (state.loadingMore) {
                                    CircularProgressIndicator()
                                } else {
                                    OutlinedButton(onClick = vm::loadMore) { Text("Load more") }
                                }
                            }
                        }
                    }
                }
            }
            Box(
                Modifier
                    .offset { IntOffset(0, tabsOffset.roundToInt()) }
                    .onSizeChanged { tabsHeight = it.height }
                    .fillMaxWidth()
                    .background(MaterialTheme.colorScheme.background)
                    .padding(start = 16.dp, end = 16.dp, bottom = 8.dp),
            ) {
                SegmentedChoice(
                    listOf(DiscoverTab.TRENDING to "Trending", DiscoverTab.POPULAR to "Popular"),
                    state.tab,
                    vm::setTab,
                )
            }
        }
    }

    if (filtersOpen) {
        FilterSheet(
            popular = popular,
            filters = state.filters,
            kind = state.kind,
            options = options,
            onKind = vm::setKind,
            onFilters = vm::setFilters,
            onClear = vm::clearFilters,
            onRetryOptions = vm::loadOptions,
            onDismiss = { filtersOpen = false },
        )
    }
}

/**
 * The filters: the kind for both lists, and under it the Popular list's own — Trending takes no
 * others, TMDB offers none there. Every change applies at once — the list reloads behind the sheet.
 * Picking a language swaps the sheet's content for the list and back, rather than stacking a
 * second sheet on the first.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun FilterSheet(
    popular: Boolean,
    filters: DiscoverFilters,
    kind: MediaKind?,
    options: DiscoverOptionsState,
    onKind: (MediaKind?) -> Unit,
    onFilters: (DiscoverFilters) -> Unit,
    onClear: () -> Unit,
    onRetryOptions: () -> Unit,
    onDismiss: () -> Unit,
) {
    var pickingLanguage by remember { mutableStateOf(false) }
    val loaded = options.options
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
    ) {
        if (popular && pickingLanguage && loaded != null) {
            LanguagePicker(loaded.languages, filters.language) {
                onFilters(filters.copy(language = it))
                pickingLanguage = false
            }
            return@ModalBottomSheet
        }
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState())
                .padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text("Filters", style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
                if (kind != null || (popular && filters.activeCount > 0)) {
                    TextButton(onClick = onClear) { Text("Clear") }
                }
            }
            FilterLabel("Show")
            SegmentedChoice(
                listOf(null to "All", MediaKind.MOVIE to "Movies", MediaKind.SHOW to "Shows"),
                kind,
                onKind,
            )
            if (!popular) return@Column
            MinScore(filters, onFilters)
            FilterLabel("Original language")
            when {
                loaded != null -> OutlinedButton(onClick = { pickingLanguage = true }, modifier = Modifier.fillMaxWidth()) {
                    Text(loaded.languages.firstOrNull { it.code == filters.language }?.name ?: filters.language ?: "Any language")
                }
                options.failed -> {
                    Text(
                        "The genre and language lists are unavailable right now — TMDB did not answer. The score filter still works.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    TextButton(onClick = onRetryOptions) { Text("Try again") }
                }
                else -> Text(
                    "Loading genres and languages…",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (loaded != null) {
                FilterLabel("Genres (all selected must match)")
                GenreChips(
                    genreOptions(kind?.let { it == MediaKind.MOVIE }, loaded.movieGenres, loaded.showGenres),
                    filters.genres,
                ) { onFilters(filters.copy(genres = it)) }
            }
        }
    }
}

@Composable
private fun FilterLabel(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(top = 8.dp),
    )
}

/**
 * The lowest TMDB score listed, in half points: 5 lists everything scored 5 or higher, and the
 * far left lists every score. The list reloads when the thumb is let go, not on every step.
 */
@Composable
private fun MinScore(filters: DiscoverFilters, onFilters: (DiscoverFilters) -> Unit) {
    var value by remember(filters.voteMin) { mutableStateOf((filters.voteMin ?: 0.0).toFloat()) }
    val score = halfPoint(value)
    FilterLabel(if (score <= 0.0) "TMDB score: any" else "TMDB score: ${scoreLabel(score)} or higher")
    Slider(
        value = value,
        onValueChange = { value = it },
        valueRange = 0f..10f,
        steps = 19, // half points: 21 stops, the two ends included
        // The thumb can rest near the screen edge, where a drag would otherwise read as "back".
        modifier = Modifier.systemGestureExclusion(),
        onValueChangeFinished = { onFilters(filters.copy(voteMin = halfPoint(value).takeIf { it > 0.0 })) },
    )
}

/** The slider's float snapped to the half point it stands on. */
private fun halfPoint(value: Float): Double = (value * 2).roundToInt() / 2.0

private fun scoreLabel(value: Double): String =
    if (value % 1.0 == 0.0) value.toInt().toString() else value.toString()

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun GenreChips(genres: List<GenreOption>, selected: Set<Long>, onChange: (Set<Long>) -> Unit) {
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        for (genre in genres) {
            val on = genre.id in selected
            FilterChip(
                selected = on,
                onClick = { onChange(if (on) selected - genre.id else selected + genre.id) },
                label = { Text(genre.name) },
            )
        }
    }
}

/** One language out of TMDB's list, with a field to narrow it; "Any language" clears the filter. */
@Composable
private fun LanguagePicker(languages: List<LanguageOption>, selected: String?, onPick: (String?) -> Unit) {
    var query by remember { mutableStateOf("") }
    val shown = remember(languages, query) {
        val q = query.trim()
        if (q.isEmpty()) languages else languages.filter { it.name.contains(q, ignoreCase = true) || it.code.equals(q, ignoreCase = true) }
    }
    Column(Modifier.fillMaxWidth().padding(start = 24.dp, end = 24.dp, bottom = 24.dp)) {
        Text("Original language", style = MaterialTheme.typography.titleLarge)
        OutlinedTextField(
            value = query,
            onValueChange = { query = it },
            singleLine = true,
            placeholder = { Text("Find a language") },
            modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp),
        )
        LazyColumn(Modifier.fillMaxWidth().heightIn(max = 420.dp)) {
            if (query.isBlank()) {
                item(key = "any") { LanguageRow("Any language", selected == null) { onPick(null) } }
            }
            items(shown, key = { it.code }) { language ->
                LanguageRow(language.name, language.code == selected) { onPick(language.code) }
            }
        }
    }
}

@Composable
private fun LanguageRow(label: String, selected: Boolean, onClick: () -> Unit) {
    Text(
        if (selected) "✓  $label" else label,
        style = MaterialTheme.typography.bodyLarge,
        fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
        color = if (selected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface,
        modifier = Modifier.fillMaxWidth().clickable(onClick = onClick).padding(vertical = 14.dp),
    )
}
