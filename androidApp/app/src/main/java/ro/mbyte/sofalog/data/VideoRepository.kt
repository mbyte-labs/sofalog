package ro.mbyte.sofalog.data

import androidx.room.withTransaction
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext
import ro.mbyte.sofalog.data.db.LibraryMarkRow
import ro.mbyte.sofalog.data.db.AppDatabase
import ro.mbyte.sofalog.data.db.CopyEntity
import ro.mbyte.sofalog.data.db.FavoriteEntity
import ro.mbyte.sofalog.data.db.WatchlistEntity
import ro.mbyte.sofalog.data.db.HistoryRow
import ro.mbyte.sofalog.data.db.ListRow
import ro.mbyte.sofalog.data.db.EpisodeEntity
import ro.mbyte.sofalog.data.db.MovieEntity
import ro.mbyte.sofalog.data.db.RatingEntity
import ro.mbyte.sofalog.data.db.SETTING_TIMEZONE
import ro.mbyte.sofalog.data.db.SETTING_WATCH_REGION
import ro.mbyte.sofalog.data.db.SYNC_DELETE
import ro.mbyte.sofalog.data.db.SYNC_FAVORITE
import ro.mbyte.sofalog.data.db.SYNC_RATING
import ro.mbyte.sofalog.data.db.SYNC_SET
import ro.mbyte.sofalog.data.db.SYNC_WATCHLIST
import ro.mbyte.sofalog.data.db.SeasonEntity
import ro.mbyte.sofalog.data.db.SettingEntity
import ro.mbyte.sofalog.data.db.enqueue
import ro.mbyte.sofalog.data.db.ShowEntity
import ro.mbyte.sofalog.data.db.ShowStateEntity
import ro.mbyte.sofalog.data.db.StatsPlayRow
import ro.mbyte.sofalog.data.db.StatsTitleRow
import ro.mbyte.sofalog.data.db.TARGET_EPISODE
import ro.mbyte.sofalog.data.db.TARGET_MOVIE
import ro.mbyte.sofalog.data.db.TARGET_SEASON
import ro.mbyte.sofalog.data.db.TARGET_SHOW
import ro.mbyte.sofalog.data.db.WatchEntity
import ro.mbyte.sofalog.data.tmdb.Fetched
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.data.tmdb.TmdbMovie
import ro.mbyte.sofalog.data.tmdb.TmdbSearchResult
import ro.mbyte.sofalog.data.tmdb.TmdbSeason
import ro.mbyte.sofalog.data.tmdb.TmdbShow
import ro.mbyte.sofalog.domain.BulkWatchMode
import ro.mbyte.sofalog.domain.DiscoverFilters
import ro.mbyte.sofalog.domain.GenreOption
import ro.mbyte.sofalog.domain.LanguageOption
import ro.mbyte.sofalog.domain.RankedRow
import ro.mbyte.sofalog.domain.discoverParams
import ro.mbyte.sofalog.domain.languageOptions
import ro.mbyte.sofalog.domain.mergeByPopularity
import ro.mbyte.sofalog.domain.trendingRowKind
import ro.mbyte.sofalog.domain.Clock
import ro.mbyte.sofalog.domain.ClockReading
import ro.mbyte.sofalog.domain.EpisodeKey
import ro.mbyte.sofalog.domain.EpisodeRef
import ro.mbyte.sofalog.domain.EpisodeResolver
import ro.mbyte.sofalog.domain.ListEntry
import ro.mbyte.sofalog.domain.ListItemKind
import ro.mbyte.sofalog.domain.ListSummary
import ro.mbyte.sofalog.domain.summarize
import ro.mbyte.sofalog.domain.MovieGroup
import ro.mbyte.sofalog.domain.RESTORE_HISTORY
import ro.mbyte.sofalog.domain.RESTORE_RATING
import ro.mbyte.sofalog.domain.RestoreCounts
import ro.mbyte.sofalog.domain.RestoreSkip
import ro.mbyte.sofalog.domain.SKIP_EPISODE_NOT_FOUND
import ro.mbyte.sofalog.domain.ShowGroup
import ro.mbyte.sofalog.domain.WATCH_REGION
import ro.mbyte.sofalog.domain.WatchRegion
import ro.mbyte.sofalog.domain.episodeSkipHint
import ro.mbyte.sofalog.domain.resolveRegion
import ro.mbyte.sofalog.domain.resolveZone
import ro.mbyte.sofalog.domain.ShowProgress
import ro.mbyte.sofalog.domain.ShowStatus
import ro.mbyte.sofalog.domain.UserZone
import ro.mbyte.sofalog.domain.isEligible
import ro.mbyte.sofalog.domain.movieSnapshotIncomplete
import ro.mbyte.sofalog.domain.parseEpisodeScores
import ro.mbyte.sofalog.domain.bulkWatchInstant
import ro.mbyte.sofalog.domain.recomputeShowStatus
import ro.mbyte.sofalog.domain.seasonSnapshotIncomplete
import ro.mbyte.sofalog.domain.showSnapshotIncomplete
import ro.mbyte.sofalog.domain.showProgress
import ro.mbyte.sofalog.domain.storedInstant
import ro.mbyte.sofalog.domain.EpisodeMarker
import ro.mbyte.sofalog.domain.LibraryMark
import ro.mbyte.sofalog.domain.LibraryMarks
import ro.mbyte.sofalog.domain.episodeMarker
import ro.mbyte.sofalog.domain.upNextEpisode
import ro.mbyte.sofalog.domain.STATS_MOVIE
import ro.mbyte.sofalog.domain.STATS_SHOW
import ro.mbyte.sofalog.domain.Stats
import ro.mbyte.sofalog.domain.StatsCompletionRow
import ro.mbyte.sofalog.domain.StatsInput
import ro.mbyte.sofalog.domain.StatsPlay
import ro.mbyte.sofalog.domain.StatsRating
import ro.mbyte.sofalog.domain.StatsTitle
import ro.mbyte.sofalog.domain.StatsTitleKey
import ro.mbyte.sofalog.domain.computeStats
import ro.mbyte.sofalog.domain.parseEpisodeRuntime
import ro.mbyte.sofalog.domain.parseVoteFacts
import ro.mbyte.sofalog.domain.splitGenres
import ro.mbyte.sofalog.domain.statsYear
import java.time.Duration
import java.time.Instant

enum class MediaKind { MOVIE, SHOW }

/** The formats a title can be owned on, in the order the page lists them. */
enum class CopyKind(val value: String, val label: String) {
    JELLYFIN("jellyfin", "Jellyfin"),
    DVD("dvd", "DVD"),
    BLURAY("bluray", "Blu-ray"),
    UHD("uhd", "4K Blu-ray"),
    PHYSICAL("physical", "Other Physical"),
    DIGITAL("digital", "Other Digital");

    companion object {
        fun of(value: String): CopyKind? = entries.firstOrNull { it.value == value }
    }
}

data class MovieUserState(
    val rating: Int? = null,
    val watchlisted: Boolean = false,
    val favorited: Boolean = false,
    val copies: Set<CopyKind> = emptySet(),
)

/** The user's own state of one show. A show's copies hang on its seasons (D-021). */
data class ShowUserState(
    val rating: Int? = null,
    val watchlisted: Boolean = false,
    val favorited: Boolean = false,
    /** Owned kinds by season number; a season with none is absent. */
    val seasonCopies: Map<Int, Set<CopyKind>> = emptyMap(),
    /** The user's episode ratings by local episode id. */
    val episodeRatings: Map<Long, Int> = emptyMap(),
)

/** The app's own settings as stored. [timeZone] null follows the device. */
data class AppSettings(val timeZone: String? = null, val watchRegion: String = WATCH_REGION)

/** What one pulled entry did: whether it fetched a new title, and whether it wrote anything. */
data class PullWrite(val materialised: Boolean, val created: Boolean, val episodeMissing: Boolean = false)

/** A restore group's title, held locally by the time it is returned. */
data class RestoreResolved(val localId: Long, val title: String, val materialised: Boolean)

/** The writes a restore needs — the seam its runner is tested through. */
interface RestoreWrites {
    suspend fun resolveRestoreMovie(tmdbId: Long, mayFetch: Boolean): RestoreResolved?
    suspend fun resolveRestoreShow(tmdbId: Long, mayFetch: Boolean): RestoreResolved?
    suspend fun writeRestoreMovie(target: RestoreResolved, group: MovieGroup): RestoreCounts
    suspend fun writeRestoreShow(target: RestoreResolved, group: ShowGroup): Pair<RestoreCounts, List<RestoreSkip>>
    suspend fun restoreTitle(movie: Boolean, tmdbId: Long): String?
}

/** One page of a trending or popular list, in TMDB's own paging. */
data class DiscoverPage(val hits: List<SearchHit>, val page: Int, val totalPages: Int)

/** What the Discover filters offer: TMDB's genres per kind and its languages. */
data class DiscoverOptions(
    val movieGenres: List<GenreOption>,
    val showGenres: List<GenreOption>,
    val languages: List<LanguageOption>,
)

data class SearchHit(
    val kind: MediaKind,
    val tmdbId: Long,
    val title: String,
    val year: String?,
    val posterPath: String?,
    val voteAverage: Double?,
)

/**
 * One show as a screen needs it. For a tracked show it is read from the database; for an
 * untracked one it is a live preview that was never stored (`show.id == 0`), and its episodes are
 * only those of the season being looked at.
 */
data class ShowBundle(
    val show: ShowEntity,
    val seasons: List<SeasonEntity>,
    val episodes: List<EpisodeEntity>,
    /** Watches by local episode id, newest first. Empty for a preview. */
    val watches: Map<Long, List<WatchEntity>>,
    val status: ShowStatus?,
    val progress: ShowProgress,
    val today: String,
) {
    val tracked: Boolean get() = show.id != 0L
}

data class UpNextItem(
    val show: ShowEntity,
    val episode: EpisodeEntity,
    val posterPath: String?,
    val lastWatchedAt: String,
    val aired: Boolean,
    /** Premiere / mid-season finale / season finale, or null for a plain episode. */
    val marker: EpisodeMarker?,
    val progress: ShowProgress,
    /** TMDB's community score of the episode; null when it has no votes or its season no stored body. */
    val score: Double? = null,
    /** This user's own rating of the episode, 1..10 — it can be rated before it is watched. */
    val userRating: Int? = null,
)

enum class HistoryType(val target: String?) { ALL(null), MOVIES(TARGET_MOVIE), EPISODES(TARGET_EPISODE) }

/** One watch in the history feed. [id] is the watch row's — a rewatch repeats the title, never the id. */
sealed interface HistoryItem {
    val id: Long
    /** The watched movie's or episode's local id. */
    val targetId: Long
    /** How many entries the whole log holds for this target, this one included. */
    val entries: Int
    val watchedAt: String
    val posterPath: String?
    /** This user's rating of the target, the same on every rewatch of it. */
    val userRating: Int?
    /** TMDB's community score of the movie or of the episode itself; null when it has no votes. */
    val score: Double?
    /** The movie's own watchlist and favorite marks; an episode borrows its show's. */
    val watchlisted: Boolean
    val favorited: Boolean

    data class Movie(
        override val id: Long,
        override val targetId: Long,
        override val entries: Int,
        override val watchedAt: String,
        override val posterPath: String?,
        override val userRating: Int?,
        override val score: Double?,
        override val watchlisted: Boolean,
        override val favorited: Boolean,
        val tmdbId: Long,
        val title: String,
    ) : HistoryItem

    data class Episode(
        override val id: Long,
        override val targetId: Long,
        override val entries: Int,
        override val watchedAt: String,
        override val posterPath: String?,
        override val userRating: Int?,
        override val score: Double?,
        override val watchlisted: Boolean,
        override val favorited: Boolean,
        /** The show's local id — what the season- and show-wide removals address. */
        val showId: Long,
        val showTmdbId: Long,
        val showTitle: String,
        val season: Int,
        val number: Int,
        val title: String?,
    ) : HistoryItem
}

data class HistoryPage(val items: List<HistoryItem>, val hasMore: Boolean)

/** The lists the Lists page offers, in its order. */
enum class ListKey { WATCHLIST, FAVORITES, DROPPED }

/**
 * The single home of the app's reads and writes (SPEC D-040). The UI reads only the local
 * database through this class; TMDB is called for search, for previews of untracked titles and
 * for materialising a title when it is first tracked — never to render something already held.
 *
 * Every write to the watch log recomputes the show's status in the same transaction, and
 * [recompute] is the only writer of `user_show_states` beside the user's own drop / un-drop
 * (`setShowDropped`).
 */
class VideoRepository(
    private val db: AppDatabase,
    private val tmdb: TmdbClient,
    private val clock: Clock,
) : RestoreWrites {
    private val dao = db.videoDao()
    private val settings = db.settingsDao()
    private val sync = db.tmdbSyncDao()

    /**
     * Called after a write that may have queued something for TMDB has committed — never inside
     * its transaction. The TMDB account service hangs its worker's kick here.
     */
    var onSyncWrite: () -> Unit = {}

    val hasTmdbKey: Boolean get() = tmdb.hasKey

    // ------------------------------------------------------------------ settings

    fun observeSettings(): Flow<AppSettings> = settings.observeAll().map { it.toSettings() }

    /**
     * Reads the stored settings into [UserZone] and [WatchRegion], the two places the rest of the
     * app reads them from. Run once at startup, before anything is shown.
     */
    suspend fun loadSettings() = applySettings(settings.all().toSettings())

    // ------------------------------------------------------------------ delete data

    /**
     * Empties the video library — history, ratings, watchlist, favorites, dropped shows, copies
     * and the stored titles. The settings and the TMDB link stay; what was still waiting to be
     * sent to TMDB is dropped with it, and nothing on TMDB is touched.
     */
    suspend fun deleteLibrary() = db.withTransaction {
        sync.deleteAllSync()
        dao.deleteLibrary()
    }

    /**
     * Back to a first launch: every table, then the zone and country the app holds in memory.
     * The TMDB link's secret lives outside the database; the caller unlinks first.
     */
    suspend fun deleteEverything() {
        withContext(Dispatchers.IO) { db.clearAllTables() }
        loadSettings()
    }

    /** [name] null follows the device's zone again. An unknown zone is refused. */
    suspend fun setTimeZone(name: String?) {
        if (name == null) {
            settings.clear(SETTING_TIMEZONE)
        } else {
            requireNotNull(resolveZone(name)) { "unknown time zone" }
            settings.set(SettingEntity(SETTING_TIMEZONE, name))
        }
        loadSettings()
    }

    /** The where-to-watch country. Each tracked title refetches once, on its next view. */
    suspend fun setWatchRegion(code: String) {
        val region = code.trim().uppercase()
        require(region.length == 2 && region.all(Char::isLetter)) { "a country is two letters" }
        settings.set(SettingEntity(SETTING_WATCH_REGION, region))
        loadSettings()
    }

    private fun applySettings(value: AppSettings) {
        UserZone.set(value.timeZone)
        WatchRegion.current = value.watchRegion
    }

    private fun List<SettingEntity>.toSettings(): AppSettings {
        val byName = associate { it.name to it.value }
        return AppSettings(
            timeZone = byName[SETTING_TIMEZONE]?.takeIf { resolveZone(it) != null },
            watchRegion = resolveRegion(byName[SETTING_WATCH_REGION]),
        )
    }

    /** Queues one change for the linked TMDB account. Must run inside the local write's transaction. */
    private suspend fun mirror(kind: String, type: String, tmdbId: Long, op: String, rating: Int?, now: String, season: Int = 0, episode: Int = 0) =
        sync.enqueue(kind, type, tmdbId, season, episode, op, rating, now)

    // ------------------------------------------------------------------ search

    /** [kind] null searches movies and shows together; a kind asks TMDB for that kind alone. */
    suspend fun search(query: String, kind: MediaKind? = null): List<SearchHit> = when (kind) {
        null -> tmdb.search(query)
        MediaKind.MOVIE -> tmdb.searchMovies(query)
        MediaKind.SHOW -> tmdb.searchShows(query)
    }.map { it.toHit(it.mediaType == "movie") }

    private fun TmdbSearchResult.toHit(movie: Boolean) = SearchHit(
        kind = if (movie) MediaKind.MOVIE else MediaKind.SHOW,
        tmdbId = id,
        title = (if (movie) title else name).orEmpty(),
        year = (if (movie) releaseDate else firstAirDate).clean()?.take(4),
        posterPath = posterPath,
        voteAverage = voteAverage,
    )

    // ------------------------------------------------------------------ discover (D-034)
    // Live on every view and never stored: a list has no local data to be over.

    /**
     * One page of this week's trending list. [kind] null is TMDB's own cross-type ranking in one
     * call, people dropped — so a page can come up short of twenty.
     */
    suspend fun trending(kind: MediaKind?, page: Int): DiscoverPage {
        val media = kind.tmdbMedia() ?: "all"
        val fetched = tmdb.trending(media, page)
        val hits = fetched.results.mapNotNull { row ->
            trendingRowKind(media, row.mediaType, row.id)?.let { row.toHit(it == "movie") }
        }
        return DiscoverPage(hits, page, fetched.totalPages)
    }

    /**
     * One page of TMDB's popular list under [filters]. [kind] null fetches both kinds' page and
     * merges them by popularity, uncapped; the page count is the longer list's.
     */
    suspend fun popular(kind: MediaKind?, filters: DiscoverFilters, page: Int): DiscoverPage {
        if (kind != null) {
            val movie = kind == MediaKind.MOVIE
            val fetched = tmdb.discover(kind.tmdbMedia()!!, discoverParams(!movie, filters, page))
            return DiscoverPage(fetched.results.filter { it.id != 0L }.map { it.toHit(movie) }, page, fetched.totalPages)
        }
        return coroutineScope {
            val movies = async { tmdb.discover("movie", discoverParams(false, filters, page)) }
            val shows = async { tmdb.discover("tv", discoverParams(true, filters, page)) }
            val rows = listOf(movies.await() to true, shows.await() to false).flatMap { (fetched, movie) ->
                fetched.results.filter { it.id != 0L }.map {
                    val hit = it.toHit(movie)
                    RankedRow(hit, it.popularity ?: 0.0, hit.title, hit.tmdbId)
                }
            }
            DiscoverPage(mergeByPopularity(rows), page, maxOf(movies.await().totalPages, shows.await().totalPages))
        }
    }

    @Volatile
    private var discoverOptionsCache: DiscoverOptions? = null

    /**
     * The filter panel's genre and language lists. They change about never, so one successful
     * read serves the process; a failure is not remembered.
     */
    suspend fun discoverOptions(): DiscoverOptions = discoverOptionsCache ?: coroutineScope {
        val movieGenres = async { tmdb.genres("movie") }
        val showGenres = async { tmdb.genres("tv") }
        val languages = async { tmdb.languages() }
        DiscoverOptions(
            movieGenres = movieGenres.await().map { GenreOption(it.id, it.name) },
            showGenres = showGenres.await().map { GenreOption(it.id, it.name) },
            languages = languageOptions(languages.await().map { it.code to it.englishName }),
        ).also { discoverOptionsCache = it }
    }

    private fun MediaKind?.tmdbMedia(): String? = when (this) {
        MediaKind.MOVIE -> "movie"
        MediaKind.SHOW -> "tv"
        null -> null
    }

    /**
     * What the library says about these search results — the user's rating and the watched,
     * watchlist and favorite marks — read from the device alone. Live: a change updates its card. A title the
     * library does not hold is simply absent.
     */
    fun observeLibraryMarks(hits: List<SearchHit>): Flow<LibraryMarks> = combine(
        dao.observeMovieMarks(hits.filter { it.kind == MediaKind.MOVIE }.map { it.tmdbId }),
        dao.observeShowMarks(hits.filter { it.kind == MediaKind.SHOW }.map { it.tmdbId }),
    ) { movies, shows ->
        LibraryMarks(movies.associate { it.tmdbId to it.toMark() }, shows.associate { it.tmdbId to it.toMark() })
    }

    private fun LibraryMarkRow.toMark() = LibraryMark(userRating, watched, watchlisted, favorited, dropped)

    // ------------------------------------------------------------------ movies

    fun observeMovie(tmdbId: Long): Flow<MovieEntity?> = dao.observeMovieByTmdbId(tmdbId)

    fun observeMovieWatches(tmdbId: Long): Flow<List<WatchEntity>> = dao.observeMovieWatches(tmdbId)

    /** A live view of an untracked movie. Stores nothing. */
    suspend fun previewMovie(tmdbId: Long): MovieEntity =
        tmdb.movie(tmdbId).toEntity(id = 0, now = clock.read().now)

    /** The user's own state of one movie. All empty while the movie is untracked. */
    @OptIn(ExperimentalCoroutinesApi::class)
    fun observeMovieUserState(tmdbId: Long): Flow<MovieUserState> =
        dao.observeMovieByTmdbId(tmdbId).map { it?.id }.distinctUntilChanged().flatMapLatest { id ->
            if (id == null) {
                flowOf(MovieUserState())
            } else {
                combine(
                    dao.observeRating(TARGET_MOVIE, id),
                    dao.observeWatchlisted(TARGET_MOVIE, id),
                    dao.observeFavorited(TARGET_MOVIE, id),
                    dao.observeCopies(TARGET_MOVIE, id),
                ) { rating, watchlisted, favorited, copies ->
                    MovieUserState(rating, watchlisted, favorited, copies.mapNotNull(CopyKind::of).toSet())
                }
            }
        }

    /**
     * The tracking gate for a movie: a first reference fetches it — outside any transaction, so
     * an outage leaves nothing half-written — and the returned writer inserts it inside the
     * caller's own. Every add (a watch, a rating, a listing, a copy) goes through here.
     */
    private suspend fun movieGate(tmdbId: Long): suspend (now: String) -> Long {
        val fetched = if (dao.movieByTmdbId(tmdbId) == null) tmdb.movie(tmdbId) else null
        return { now ->
            dao.movieByTmdbId(tmdbId)?.id ?: dao.insertMovie(checkNotNull(fetched).toEntity(id = 0, now = now))
        }
    }

    /**
     * Logs a watch, materialising the movie first when this is its first reference. [watchedAt]
     * null means now; a past instant backdates the watch and nothing else, a future one is
     * refused. [rating] (1..10) lands in the same transaction; null leaves the rating alone.
     */
    suspend fun markMovieWatched(tmdbId: Long, watchedAt: Instant? = null, rating: Int? = null) {
        require(rating == null || rating in 1..10) { "rating must be 1..10" }
        val ensure = movieGate(tmdbId)
        val reading = clock.read()
        require(watchedAt == null || !watchedAt.isAfter(Instant.parse(reading.now))) { "watched_at is in the future" }
        val at = watchedAt?.let(::storedInstant) ?: reading.now
        db.withTransaction {
            val movieId = ensure(reading.now)
            dao.insertWatch(WatchEntity(targetType = TARGET_MOVIE, targetId = movieId, watchedAt = at))
            rating?.let {
                dao.upsertRating(RatingEntity(TARGET_MOVIE, movieId, it, reading.now))
                mirror(SYNC_RATING, TARGET_MOVIE, tmdbId, SYNC_SET, it, reading.now)
            }
        }
        onSyncWrite()
    }

    /** Sets or — with null — clears the rating. Rating is a tracking action; clearing is not. */
    suspend fun rateMovie(tmdbId: Long, rating: Int?) {
        require(rating == null || rating in 1..10) { "rating must be 1..10" }
        if (rating == null) {
            removeFromMovie(tmdbId) { id, now ->
                dao.deleteRating(TARGET_MOVIE, id)
                mirror(SYNC_RATING, TARGET_MOVIE, tmdbId, SYNC_DELETE, null, now)
            }
        } else {
            addToMovie(tmdbId) { id, now ->
                dao.upsertRating(RatingEntity(TARGET_MOVIE, id, rating, now))
                mirror(SYNC_RATING, TARGET_MOVIE, tmdbId, SYNC_SET, rating, now)
            }
        }
    }

    suspend fun setMovieWatchlisted(tmdbId: Long, on: Boolean) = if (on) {
        addToMovie(tmdbId) { id, now ->
            dao.insertWatchlist(WatchlistEntity(TARGET_MOVIE, id, now))
            mirror(SYNC_WATCHLIST, TARGET_MOVIE, tmdbId, SYNC_SET, null, now)
        }
    } else {
        removeFromMovie(tmdbId) { id, now ->
            dao.deleteWatchlist(TARGET_MOVIE, id)
            mirror(SYNC_WATCHLIST, TARGET_MOVIE, tmdbId, SYNC_DELETE, null, now)
        }
    }

    suspend fun setMovieFavorited(tmdbId: Long, on: Boolean) = if (on) {
        addToMovie(tmdbId) { id, now ->
            dao.insertFavorite(FavoriteEntity(TARGET_MOVIE, id, now))
            mirror(SYNC_FAVORITE, TARGET_MOVIE, tmdbId, SYNC_SET, null, now)
        }
    } else {
        removeFromMovie(tmdbId) { id, now ->
            dao.deleteFavorite(TARGET_MOVIE, id)
            mirror(SYNC_FAVORITE, TARGET_MOVIE, tmdbId, SYNC_DELETE, null, now)
        }
    }

    suspend fun setMovieCopy(tmdbId: Long, kind: CopyKind, owned: Boolean) = if (owned) {
        addToMovie(tmdbId) { id, now -> dao.insertCopy(CopyEntity(TARGET_MOVIE, id, kind.value, now)) }
    } else {
        removeFromMovie(tmdbId) { id, _ -> dao.deleteCopy(TARGET_MOVIE, id, kind.value) }
    }

    private suspend fun addToMovie(tmdbId: Long, write: suspend (movieId: Long, now: String) -> Unit) {
        val ensure = movieGate(tmdbId)
        val now = clock.read().now
        db.withTransaction { write(ensure(now), now) }
        onSyncWrite()
    }

    /** A removal never fetches: an untracked movie has nothing to remove. The catalog row stays. */
    private suspend fun removeFromMovie(tmdbId: Long, write: suspend (movieId: Long, now: String) -> Unit) {
        val now = clock.read().now
        db.withTransaction { dao.movieByTmdbId(tmdbId)?.let { write(it.id, now) } }
        onSyncWrite()
    }

    /**
     * Refreshes a tracked movie whose snapshot is older than [STALE_AFTER], or predates a section
     * the page reads (which is how a movie tracked by an older build fills in, once). An outage
     * is the caller's to swallow: the stored snapshot keeps rendering.
     */
    suspend fun refreshMovieIfStale(tmdbId: Long) {
        val movie = dao.movieByTmdbId(tmdbId) ?: return
        val age = runCatching { Duration.between(Instant.parse(movie.fetchedAt), Instant.now()) }.getOrNull()
        if (age != null && age < STALE_AFTER && !movieSnapshotIncomplete(movie.metadataJson)) return
        val fetched = tmdb.movie(tmdbId)
        dao.updateMovie(fetched.toEntity(id = movie.id, now = clock.read().now))
    }

    // ------------------------------------------------------------------ shows

    @OptIn(ExperimentalCoroutinesApi::class)
    fun observeShow(tmdbId: Long): Flow<ShowBundle?> =
        dao.observeShowByTmdbId(tmdbId).flatMapLatest { show ->
            if (show == null) {
                flowOf(null)
            } else {
                combine(
                    dao.observeSeasons(show.id),
                    dao.observeEpisodes(show.id),
                    dao.observeShowWatches(show.id),
                    dao.observeShowState(show.id),
                ) { seasons, episodes, watches, state ->
                    val today = clock.read().today
                    val byEpisode = watches.groupBy { it.targetId }
                    ShowBundle(
                        show = show,
                        seasons = seasons,
                        episodes = episodes,
                        watches = byEpisode,
                        status = ShowStatus.of(state?.status),
                        progress = showProgress(episodes.map { it.ref() }, byEpisode.keys, today),
                        today = today,
                    )
                }
            }
        }

    /**
     * A live view of an untracked show: the root document, plus one season's episodes when a
     * season page is asking. Stores nothing — tracking is what materialises a show.
     */
    suspend fun previewShow(tmdbId: Long, seasonNumber: Int?): ShowBundle {
        val reading = clock.read()
        val root = tmdb.show(tmdbId)
        val season = seasonNumber?.let { tmdb.season(tmdbId, it) }
        return ShowBundle(
            show = root.toEntity(id = 0, now = reading.now),
            // The season being looked at carries its own snapshot, as a stored one would.
            seasons = root.value.seasons.map {
                val row = it.toEntity(showId = 0, id = 0)
                if (season != null && it.seasonNumber == seasonNumber) {
                    row.copy(metadataJson = season.body, fetchedAt = reading.now)
                } else {
                    row
                }
            },
            episodes = season?.value?.episodes.orEmpty().map { it.toEntity(0, 0, seasonNumber ?: 0, 0) },
            watches = emptyMap(),
            status = null,
            progress = ShowProgress(0, 0),
            today = reading.today,
        )
    }

    /**
     * The tracking gate: returns the local id of a show, materialising the show, every season
     * and every episode on first reference. All fetching happens before the one transaction, so
     * an outage leaves nothing half-written.
     */
    suspend fun trackShow(tmdbId: Long): Long {
        dao.showByTmdbId(tmdbId)?.let { return it.id }
        val root = tmdb.show(tmdbId)
        val seasons = root.value.seasons.map { tmdb.season(tmdbId, it.seasonNumber) }
        val now = clock.read().now
        return db.withTransaction {
            dao.showByTmdbId(tmdbId)?.id ?: run {
                val showId = dao.insertShow(root.toEntity(id = 0, now = now))
                seasons.forEach { writeSeason(showId, it, root.value, now) }
                showId
            }
        }
    }

    /** The user's own state of one show. All empty while the show is untracked. */
    @OptIn(ExperimentalCoroutinesApi::class)
    fun observeShowUserState(tmdbId: Long): Flow<ShowUserState> =
        dao.observeShowByTmdbId(tmdbId).map { it?.id }.distinctUntilChanged().flatMapLatest { id ->
            if (id == null) {
                flowOf(ShowUserState())
            } else {
                combine(
                    dao.observeRating(TARGET_SHOW, id),
                    dao.observeWatchlisted(TARGET_SHOW, id),
                    dao.observeFavorited(TARGET_SHOW, id),
                    dao.observeSeasonCopies(id),
                    dao.observeEpisodeRatings(id),
                ) { rating, watchlisted, favorited, copies, episodeRatings ->
                    ShowUserState(
                        rating = rating,
                        watchlisted = watchlisted,
                        favorited = favorited,
                        seasonCopies = copies.groupBy({ it.seasonNumber }, { CopyKind.of(it.kind) })
                            .mapValues { (_, kinds) -> kinds.filterNotNull().toSet() },
                        episodeRatings = episodeRatings.associate { it.episodeId to it.rating },
                    )
                }
            }
        }

    /** Sets or — with null — clears the show's rating. Rating is a tracking action; clearing is not. */
    suspend fun rateShow(tmdbId: Long, rating: Int?) {
        require(rating == null || rating in 1..10) { "rating must be 1..10" }
        if (rating == null) {
            removeFromShow(tmdbId) { id, now ->
                dao.deleteRating(TARGET_SHOW, id)
                mirror(SYNC_RATING, TARGET_SHOW, tmdbId, SYNC_DELETE, null, now)
            }
        } else {
            addToShow(tmdbId) { id, now ->
                dao.upsertRating(RatingEntity(TARGET_SHOW, id, rating, now))
                mirror(SYNC_RATING, TARGET_SHOW, tmdbId, SYNC_SET, rating, now)
            }
        }
    }

    /**
     * Sets or — with null — clears one episode's rating without logging a watch. Rating is a
     * tracking action, so an untracked show is fetched first; clearing never fetches.
     */
    suspend fun rateEpisode(showTmdbId: Long, seasonNumber: Int, number: Int, rating: Int?) {
        require(rating == null || rating in 1..10) { "rating must be 1..10" }
        if (rating == null) {
            removeFromShow(showTmdbId) { id, now ->
                dao.episodeByKey(id, seasonNumber, number)?.let {
                    dao.deleteRating(TARGET_EPISODE, it.id)
                    mirror(SYNC_RATING, TARGET_EPISODE, showTmdbId, SYNC_DELETE, null, now, seasonNumber, number)
                }
            }
        } else {
            addToShow(showTmdbId) { id, now ->
                val episode = dao.episodeByKey(id, seasonNumber, number)
                    ?: throw NoSuchElementException("episode $seasonNumber x $number")
                dao.upsertRating(RatingEntity(TARGET_EPISODE, episode.id, rating, now))
                mirror(SYNC_RATING, TARGET_EPISODE, showTmdbId, SYNC_SET, rating, now, seasonNumber, number)
            }
        }
    }

    suspend fun setShowWatchlisted(tmdbId: Long, on: Boolean) = if (on) {
        addToShow(tmdbId) { id, now ->
            dao.insertWatchlist(WatchlistEntity(TARGET_SHOW, id, now))
            mirror(SYNC_WATCHLIST, TARGET_SHOW, tmdbId, SYNC_SET, null, now)
        }
    } else {
        removeFromShow(tmdbId) { id, now ->
            dao.deleteWatchlist(TARGET_SHOW, id)
            mirror(SYNC_WATCHLIST, TARGET_SHOW, tmdbId, SYNC_DELETE, null, now)
        }
    }

    suspend fun setShowFavorited(tmdbId: Long, on: Boolean) = if (on) {
        addToShow(tmdbId) { id, now ->
            dao.insertFavorite(FavoriteEntity(TARGET_SHOW, id, now))
            mirror(SYNC_FAVORITE, TARGET_SHOW, tmdbId, SYNC_SET, null, now)
        }
    } else {
        removeFromShow(tmdbId) { id, now ->
            dao.deleteFavorite(TARGET_SHOW, id)
            mirror(SYNC_FAVORITE, TARGET_SHOW, tmdbId, SYNC_DELETE, null, now)
        }
    }

    /**
     * The show-wide copy action: a fan-out over every NON-SPECIAL season, since a show is never
     * itself owned. Specials are managed from their own season.
     */
    suspend fun setShowCopy(tmdbId: Long, kind: CopyKind, owned: Boolean) = if (owned) {
        addToShow(tmdbId) { id, now ->
            dao.seasons(id).filter { it.seasonNumber > 0 }
                .forEach { dao.insertCopy(CopyEntity(TARGET_SEASON, it.id, kind.value, now)) }
        }
    } else {
        removeFromShow(tmdbId) { id, _ ->
            dao.seasons(id).filter { it.seasonNumber > 0 }
                .forEach { dao.deleteCopy(TARGET_SEASON, it.id, kind.value) }
        }
    }

    /** An add tracks the show first ([trackShow] fetches outside the transaction). */
    private suspend fun addToShow(tmdbId: Long, write: suspend (showId: Long, now: String) -> Unit) {
        val showId = trackShow(tmdbId)
        val now = clock.read().now
        db.withTransaction { write(showId, now) }
        onSyncWrite()
    }

    /** A removal never fetches: an untracked show has nothing to remove. The catalog rows stay. */
    private suspend fun removeFromShow(tmdbId: Long, write: suspend (showId: Long, now: String) -> Unit) {
        val now = clock.read().now
        db.withTransaction { dao.showByTmdbId(tmdbId)?.let { write(it.id, now) } }
        onSyncWrite()
    }

    /**
     * Drops a show, or takes the drop back. Neither is a tracking action: only a started show —
     * one with a state row — can be dropped, so nothing is fetched. Dropping is sticky through
     * every recompute. Un-dropping lands on `watching` and recomputes in the same transaction,
     * which is what brings a finished show back as watched and collapses one whose watches are gone.
     */
    suspend fun setShowDropped(tmdbId: Long, dropped: Boolean) {
        val reading = clock.read()
        db.withTransaction {
            val show = dao.showByTmdbId(tmdbId) ?: return@withTransaction
            val state = dao.showState(show.id)
            check(state != null) { "a show that was never started cannot be dropped" }
            val isDropped = state.status == ShowStatus.DROPPED.value
            if (dropped && !isDropped) {
                dao.updateShowStateStatus(show.id, ShowStatus.DROPPED.value, reading.now)
            } else if (!dropped && isDropped) {
                dao.updateShowStateStatus(show.id, ShowStatus.WATCHING.value, reading.now)
                recompute(show.id, reading)
            }
        }
    }

    /**
     * Refreshes a tracked show whose snapshot is older than [STALE_AFTER], or predates a section
     * the page reads (which is how a show tracked by an older build fills in, once): the root, any season
     * it did not have, and its open seasons (the latest, plus any with an undated or future
     * episode). A closed season is not re-fetched. Episodes TMDB no longer lists are kept — they
     * may carry watches. Then the status is recomputed, which is what notices a newly aired
     * episode.
     */
    suspend fun refreshShowIfStale(tmdbId: Long) {
        val show = dao.showByTmdbId(tmdbId) ?: return
        val age = runCatching { Duration.between(Instant.parse(show.fetchedAt), Instant.now()) }.getOrNull()
        if (age != null && age < STALE_AFTER && !showSnapshotIncomplete(show.metadataJson)) {
            db.withTransaction { recompute(show.id, clock.read()) }
            return
        }
        val root = tmdb.show(tmdbId)
        val reading = clock.read()
        val held = dao.seasons(show.id).map { it.seasonNumber }.toSet()
        val episodes = dao.episodes(show.id)
        val latest = root.value.seasons.maxOfOrNull { it.seasonNumber }
        val open = episodes
            .filter { it.seasonNumber > 0 && (it.airDate == null || it.airDate > reading.today) }
            .map { it.seasonNumber }.toSet()
        val wanted = root.value.seasons.map { it.seasonNumber }
            .filter { it !in held || it == latest || it in open }
        val seasons = wanted.map { tmdb.season(tmdbId, it) }
        db.withTransaction {
            dao.updateShow(root.toEntity(id = show.id, now = reading.now))
            seasons.forEach { writeSeason(show.id, it, root.value, reading.now) }
            recompute(show.id, reading)
        }
    }

    /**
     * Refreshes one season of a tracked show when its own snapshot is missing — every season
     * stored before the body was kept, which is how those fill in, once — predates a section the
     * pages read ([seasonSnapshotIncomplete]), or is older than [STALE_AFTER]. It is the season page's refresh: the show's own leaves a closed season alone.
     * An outage is the caller's to swallow; the stored season keeps rendering.
     */
    suspend fun refreshSeasonIfStale(tmdbId: Long, seasonNumber: Int) {
        val show = dao.showByTmdbId(tmdbId) ?: return
        val season = dao.seasons(show.id).firstOrNull { it.seasonNumber == seasonNumber } ?: return
        val age = season.fetchedAt?.let { runCatching { Duration.between(Instant.parse(it), Instant.now()) }.getOrNull() }
        val body = season.metadataJson
        if (body != null && age != null && age < STALE_AFTER && !seasonSnapshotIncomplete(body)) return
        val fetched = tmdb.season(tmdbId, seasonNumber)
        val reading = clock.read()
        db.withTransaction {
            writeSeason(show.id, fetched, root = null, now = reading.now)
            recompute(show.id, reading)
        }
    }

    /**
     * One season's copy (D-021) — Specials included, which the show-wide action leaves out.
     * Adding is a tracking action; removing never fetches.
     */
    suspend fun setSeasonCopy(tmdbId: Long, seasonNumber: Int, kind: CopyKind, owned: Boolean) = if (owned) {
        addToShow(tmdbId) { id, now ->
            val season = dao.seasons(id).firstOrNull { it.seasonNumber == seasonNumber }
                ?: throw NoSuchElementException("season $seasonNumber")
            dao.insertCopy(CopyEntity(TARGET_SEASON, season.id, kind.value, now))
        }
    } else {
        removeFromShow(tmdbId) { id, _ ->
            dao.seasons(id).firstOrNull { it.seasonNumber == seasonNumber }
                ?.let { dao.deleteCopy(TARGET_SEASON, it.id, kind.value) }
        }
    }

    /**
     * Upserts one season and its episodes by their natural keys, keeping local ids stable.
     * [root] lends the show's own poster for a season whose document has none; without it the
     * stored one is kept.
     */
    private suspend fun writeSeason(showId: Long, fetched: Fetched<TmdbSeason>, root: TmdbShow?, now: String) {
        val season = fetched.value
        val summary = root?.seasons?.firstOrNull { it.seasonNumber == season.seasonNumber }
        val existing = dao.seasons(showId).firstOrNull { it.seasonNumber == season.seasonNumber }
        val row = SeasonEntity(
            id = existing?.id ?: 0,
            showId = showId,
            seasonNumber = season.seasonNumber,
            name = season.name.clean(),
            airDate = season.airDate.clean(),
            overview = season.overview.clean(),
            posterPath = season.posterPath ?: summary?.posterPath ?: existing?.posterPath,
            episodeCount = season.episodes.size,
            metadataJson = fetched.body,
            fetchedAt = now,
        )
        val seasonId = if (existing == null) dao.insertSeason(row) else row.id.also { dao.updateSeason(row) }
        for (episode in season.episodes) {
            val held = dao.episodeByKey(showId, season.seasonNumber, episode.episodeNumber)
            val entity = episode.toEntity(showId, seasonId, season.seasonNumber, held?.id ?: 0)
            if (held == null) dao.insertEpisode(entity) else dao.updateEpisode(entity)
        }
    }

    // ------------------------------------------------------------------ watches

    /**
     * Logs one episode watch. Tracks the show first when it is not yet held.
     *
     * [watchedAt] null means now; a past instant backdates the watch and nothing else — the
     * rating's timestamp and the recompute keep the real clock, or "aired" would be decided as of
     * then. A future instant is refused. [rating] (1..10), when given, lands in the same
     * transaction and overwrites the episode's rating; null leaves it alone.
     */
    suspend fun markEpisodeWatched(
        showTmdbId: Long,
        season: Int,
        number: Int,
        watchedAt: Instant? = null,
        rating: Int? = null,
    ) {
        require(rating == null || rating in 1..10) { "rating must be 1..10" }
        val showId = trackShow(showTmdbId)
        val reading = clock.read()
        require(watchedAt == null || !watchedAt.isAfter(Instant.parse(reading.now))) { "watched_at is in the future" }
        val at = watchedAt?.let(::storedInstant) ?: reading.now
        db.withTransaction {
            val episode = dao.episodeByKey(showId, season, number)
                ?: throw NoSuchElementException("episode S${season}E$number")
            dao.insertWatch(WatchEntity(targetType = TARGET_EPISODE, targetId = episode.id, watchedAt = at))
            rating?.let {
                dao.upsertRating(RatingEntity(TARGET_EPISODE, episode.id, it, reading.now))
                mirror(SYNC_RATING, TARGET_EPISODE, showTmdbId, SYNC_SET, it, reading.now, season, number)
            }
            recompute(showId, reading)
        }
        onSyncWrite()
    }

    /**
     * The bulk mark: every aired, non-special episode of the show — or of one season — gets a
     * watch, and the status is recomputed once. Returns how many were marked. The eligibility is
     * D-012's predicate, the same one the recompute uses.
     *
     * [rewatch] false marks only the episodes not watched yet; true gives every eligible episode
     * a NEW entry. [mode] says when they were watched; it moves `watched_at` and nothing else —
     * "aired" is still decided by the real clock. A mode that cannot date every episode (a season
     * premiere mode with an unusable premiere and no fallback) is refused before anything is written.
     */
    suspend fun markAiredWatched(
        showTmdbId: Long,
        season: Int?,
        rewatch: Boolean = false,
        mode: BulkWatchMode = BulkWatchMode.Now,
    ): Int {
        val showId = trackShow(showTmdbId)
        val reading = clock.read()
        val now = Instant.parse(reading.now)
        val picked = when (mode) {
            is BulkWatchMode.Fixed -> mode.at
            is BulkWatchMode.SeasonAir -> mode.fallback
            else -> null
        }
        require(picked == null || !picked.isAfter(now)) { "watched_at is in the future" }
        val zone = UserZone.current
        return db.withTransaction {
            val watched = dao.watchedEpisodeIds(showId).toSet()
            val premieres = dao.seasons(showId).associate { it.seasonNumber to it.airDate }
            val targets = dao.episodes(showId).filter {
                (season == null || it.seasonNumber == season) &&
                    it.ref().isEligible(reading.today) && (rewatch || it.id !in watched)
            }
            val watches = targets.map {
                val at = bulkWatchInstant(mode, it.airDate, premieres[it.seasonNumber], reading.today, zone, reading.now)
                requireNotNull(at) { "season ${it.seasonNumber} has no premiere date and no fallback was given" }
                WatchEntity(targetType = TARGET_EPISODE, targetId = it.id, watchedAt = at)
            }
            dao.insertWatches(watches)
            recompute(showId, reading)
            targets.size
        }
    }

    /** Removes one watch entry and recomputes in the same transaction, as marking does. */
    suspend fun deleteWatch(watchId: Long) {
        val reading = clock.read()
        db.withTransaction {
            val watch = dao.watchById(watchId) ?: return@withTransaction
            dao.deleteWatch(watchId)
            if (watch.targetType == TARGET_EPISODE) {
                dao.episodeById(watch.targetId)?.let { recompute(it.showId, reading) }
            }
        }
    }

    /*
     * The history page's wider removals. Each takes local ids, removes every matching entry and —
     * for episodes — recomputes the show once in the same transaction, as [deleteWatch] does. The
     * catalog rows stay. A movie has no stored state, so its removal has nothing to recompute.
     */

    suspend fun removeMovieFromHistory(movieId: Long) {
        db.withTransaction { dao.deleteTargetWatches(TARGET_MOVIE, movieId) }
    }

    suspend fun removeEpisodeFromHistory(episodeId: Long) {
        val reading = clock.read()
        db.withTransaction {
            dao.deleteTargetWatches(TARGET_EPISODE, episodeId)
            dao.episodeById(episodeId)?.let { recompute(it.showId, reading) }
        }
    }

    /** Every watch of every episode of one season of the show. */
    suspend fun removeSeasonFromHistory(showId: Long, season: Int) {
        val reading = clock.read()
        db.withTransaction {
            dao.deleteSeasonWatches(showId, season)
            recompute(showId, reading)
        }
    }

    /** Every episode watch of the show, specials included. */
    suspend fun removeShowFromHistory(showId: Long) {
        val reading = clock.read()
        db.withTransaction {
            dao.deleteShowWatches(showId)
            recompute(showId, reading)
        }
    }

    /** The single writer of `user_show_states` (D-010). Must run inside a transaction. */
    private suspend fun recompute(showId: Long, reading: ClockReading) {
        val current = dao.showState(showId)
        val next = recomputeShowStatus(
            current = ShowStatus.of(current?.status),
            episodes = dao.episodes(showId).map { it.ref() },
            watched = dao.watchedEpisodeIds(showId).toSet(),
            today = reading.today,
        )
        when {
            next == null -> if (current != null) dao.deleteShowState(showId)
            next.value != current?.status ->
                dao.upsertShowState(ShowStateEntity(showId, next.value, reading.now))
        }
    }

    // ------------------------------------------------------------------ up next

    /**
     * One entry per started, not-dropped show with an episode after its highest watched one,
     * most recently watched first. A pure local read.
     */
    /** Whether anything was ever logged: what tells an empty Up Next "new here" from "caught up". */
    fun observeHasWatches(): Flow<Boolean> = dao.observeHasWatches()

    fun observeUpNext(): Flow<List<UpNextItem>> =
        combine(
            dao.observeStartedShows(),
            dao.observeShowStates(),
            dao.observeAllEpisodeRatings(),
        ) { started, states, ratings ->
            val rated = ratings.associate { it.episodeId to it.rating }
            val today = clock.read().today
            val ids = started.map { it.showId }
            val shows = dao.showsByIds(ids).associateBy { it.id }
            val episodes = dao.episodesForShows(ids).groupBy { it.showId }
            val seasons = dao.seasonsForShows(ids).associateBy { it.id }
            val watched = dao.allWatchedEpisodeIds().toSet()
            val status = states.associate { it.showId to ShowStatus.of(it.status) }
            started.mapNotNull { row ->
                val show = shows[row.showId] ?: return@mapNotNull null
                val all = episodes[row.showId].orEmpty()
                val refs = all.map { it.ref() }
                val pick = upNextEpisode(status[row.showId], refs, watched) ?: return@mapNotNull null
                val episode = all.first { it.id == pick.id }
                UpNextItem(
                    show = show,
                    episode = episode,
                    posterPath = seasons[episode.seasonId]?.posterPath ?: show.posterPath,
                    lastWatchedAt = row.lastWatchedAt,
                    aired = pick.isEligible(today),
                    marker = episodeMarker(episode.episodeType, episode.episodeNumber),
                    progress = showProgress(refs, watched, today),
                    // Out of the season's stored body, as the episode page and the history feed read it.
                    score = parseEpisodeScores(seasons[episode.seasonId]?.metadataJson)[episode.episodeNumber],
                    userRating = rated[episode.id],
                )
            }.sortedWith(
                compareByDescending<UpNextItem> { it.lastWatchedAt }
                    .thenByDescending { it.show.title.lowercase() }
                    .thenByDescending { it.show.id },
            )
        }

    // ------------------------------------------------------------------ history

    /**
     * The first [limit] watches of the feed, live: a watch logged or removed anywhere re-emits.
     * One row more than asked is read, which is how [HistoryPage.hasMore] is known without a count.
     */
    fun observeHistory(type: HistoryType, newestFirst: Boolean, limit: Int): Flow<HistoryPage> {
        val rows = if (newestFirst) {
            dao.observeHistoryNewest(type.target, limit + 1)
        } else {
            dao.observeHistoryOldest(type.target, limit + 1)
        }
        return rows.map { list ->
            val shown = list.take(limit)
            // An episode's score lives in its season's stored body, as the episode page reads it:
            // one read and one parse per season on the page.
            val scores = dao.seasonBodies(shown.mapNotNull { it.seasonId }.distinct())
                .associate { it.id to parseEpisodeScores(it.metadataJson) }
            HistoryPage(shown.mapNotNull { it.toItem(scores) }, hasMore = list.size > limit)
        }
    }

    // ------------------------------------------------------------------ lists

    /**
     * One list, whole and unordered — `orderList` is its page's order. Live: a title added,
     * removed, dropped or watched anywhere re-emits. A pure local read.
     */
    fun observeList(key: ListKey): Flow<List<ListEntry>> = when (key) {
        ListKey.WATCHLIST -> dao.observeWatchlistRows()
        ListKey.FAVORITES -> dao.observeFavoriteRows()
        ListKey.DROPPED -> dao.observeDroppedRows()
    }.map { rows -> rows.map { it.toEntry() } }

    /** Every list's card for the Lists page: all three, always, in [ListKey]'s order. */
    fun observeListSummaries(): Flow<List<Pair<ListKey, ListSummary>>> =
        combine(ListKey.entries.map { key -> observeList(key).map { key to summarize(it) } }) { it.toList() }

    // ------------------------------------------------------------------ stats

    /**
     * The whole Stats page for one [period] (D-039) — null for the default, "all", or a four-digit
     * year. Live: a watch, a rating or a drop anywhere re-emits. A pure local read: the log and
     * the ratings are read whole and folded by [computeStats] in the user's zone, off the main
     * thread; the stored TMDB bodies answer what TMDB's averages and regular runtimes are.
     */
    fun observeStats(period: String?): Flow<Stats> =
        combine(
            dao.observeStatsPlays(),
            dao.observeStatsRatings(),
            dao.observeShowStates(),
        ) { plays, ratings, states ->
            val reading = clock.read()
            val titles = HashMap<StatsTitleKey, StatsTitle>()
            dao.statsMovieTitles().forEach { titles[StatsTitleKey(STATS_MOVIE, it.id)] = it.toStatsTitle(STATS_MOVIE) }
            dao.statsShowTitles().forEach { titles[StatsTitleKey(STATS_SHOW, it.id)] = it.toStatsTitle(STATS_SHOW) }

            val showBodies = dao.statsShowBodies()
            val votes = HashMap<StatsTitleKey, ro.mbyte.sofalog.domain.StatsVoteFacts>()
            dao.statsRatedMovieBodies().forEach { row ->
                parseVoteFacts(row.metadataJson)?.let { votes[StatsTitleKey(STATS_MOVIE, row.id)] = it }
            }
            showBodies.forEach { row ->
                parseVoteFacts(row.metadataJson)?.let { votes[StatsTitleKey(STATS_SHOW, row.id)] = it }
            }
            val fallbacks = showBodies.mapNotNull { row -> parseEpisodeRuntime(row.metadataJson)?.let { row.id to it } }.toMap()

            // Followed, non-dropped shows: progress is derived, as everywhere (D-010, D-012).
            val followed = states.filter { ShowStatus.of(it.status) != ShowStatus.DROPPED }.map { it.showId }
            val episodes = dao.episodesForShows(followed).groupBy { it.showId }
            val watched = dao.allWatchedEpisodeIds().toSet()
            val completion = followed.map { showId ->
                val progress = showProgress(episodes[showId].orEmpty().map { it.ref() }, watched, reading.today)
                StatsCompletionRow(showId, aired = progress.airedTotal, watched = progress.watched)
            }

            computeStats(
                StatsInput(
                    plays = plays.map { it.toStatsPlay() },
                    ratings = ratings.map { StatsRating(it.targetType, it.targetId, it.rating, it.updatedAt, it.present) },
                    titles = titles,
                    showRuntimeFallback = fallbacks,
                    voteFacts = votes,
                    completion = completion,
                ),
                period,
                Instant.parse(reading.now),
                UserZone.current,
            )
        }.flowOn(Dispatchers.Default)

    // ------------------------------------------------------------------ the TMDB account's pull
    // (D-036). What the setup imports from the account lands through the ordinary tracking gate —
    // and is NEVER queued back: none of these calls `mirror`, so a pull cannot echo to TMDB.

    /** Overwrites the rating of a movie or show with the account's. */
    suspend fun pullRating(kind: MediaKind, tmdbId: Long, rating: Int): PullWrite {
        require(rating in 1..10) { "rating must be 1..10" }
        if (kind == MediaKind.MOVIE) {
            val held = dao.movieByTmdbId(tmdbId) != null
            val ensure = movieGate(tmdbId)
            val now = clock.read().now
            db.withTransaction { dao.upsertRating(RatingEntity(TARGET_MOVIE, ensure(now), rating, now)) }
            return PullWrite(materialised = !held, created = true)
        }
        val held = dao.showByTmdbId(tmdbId) != null
        val showId = trackShow(tmdbId)
        val now = clock.read().now
        db.withTransaction { dao.upsertRating(RatingEntity(TARGET_SHOW, showId, rating, now)) }
        return PullWrite(materialised = !held, created = true)
    }

    /** An episode TMDB no longer lists at that position is reported, not failed. */
    suspend fun pullEpisodeRating(showTmdbId: Long, season: Int, number: Int, rating: Int): PullWrite {
        require(rating in 1..10) { "rating must be 1..10" }
        val held = dao.showByTmdbId(showTmdbId) != null
        val showId = trackShow(showTmdbId)
        val now = clock.read().now
        val written = db.withTransaction {
            val episode = dao.episodeByKey(showId, season, number) ?: return@withTransaction false
            dao.upsertRating(RatingEntity(TARGET_EPISODE, episode.id, rating, now))
            true
        }
        return PullWrite(materialised = !held, created = written, episodeMissing = !written)
    }

    /** Adds a movie or show to the watchlist ([favorite] false) or the favorites, if absent. */
    suspend fun pullListed(favorite: Boolean, kind: MediaKind, tmdbId: Long): PullWrite {
        val movie = kind == MediaKind.MOVIE
        val type = if (movie) TARGET_MOVIE else TARGET_SHOW
        val held = if (movie) dao.movieByTmdbId(tmdbId) != null else dao.showByTmdbId(tmdbId) != null
        val ensure = if (movie) movieGate(tmdbId) else null
        val showId = if (movie) null else trackShow(tmdbId)
        val now = clock.read().now
        val created = db.withTransaction {
            val id = showId ?: checkNotNull(ensure)(now)
            val row = if (favorite) {
                dao.insertFavorite(FavoriteEntity(type, id, now))
            } else {
                dao.insertWatchlist(WatchlistEntity(type, id, now))
            }
            row != -1L
        }
        return PullWrite(materialised = !held, created = created)
    }

    // ------------------------------------------------------------------ restore (D-023)
    // One Ticket Box group at a time: resolve outside any transaction, then one transaction and at
    // most one recompute. The file's instants move `watched_at` and a rating's `updated_at`;
    // everything else keeps the real clock.

    /**
     * The movie a restore group names. Null when it is not held and [mayFetch] is false (TMDB has
     * been failing); throws [ro.mbyte.sofalog.data.tmdb.TmdbException] when the fetch fails.
     */
    override suspend fun resolveRestoreMovie(tmdbId: Long, mayFetch: Boolean): RestoreResolved? {
        dao.movieByTmdbId(tmdbId)?.let { return RestoreResolved(it.id, it.title, materialised = false) }
        if (!mayFetch) return null
        val fetched = tmdb.movie(tmdbId)
        val now = clock.read().now
        // Its own small transaction, as the show's materialisation is: the group's writes follow.
        val id = db.withTransaction {
            dao.movieByTmdbId(tmdbId)?.id ?: dao.insertMovie(fetched.toEntity(id = 0, now = now))
        }
        return RestoreResolved(id, fetched.value.title, materialised = true)
    }

    override suspend fun resolveRestoreShow(tmdbId: Long, mayFetch: Boolean): RestoreResolved? {
        dao.showByTmdbId(tmdbId)?.let { return RestoreResolved(it.id, it.title, materialised = false) }
        if (!mayFetch) return null
        val id = trackShow(tmdbId)
        return RestoreResolved(id, dao.showById(id)?.title.orEmpty(), materialised = true)
    }

    override suspend fun writeRestoreMovie(target: RestoreResolved, group: MovieGroup): RestoreCounts {
        val now = clock.read().now
        val movieId = target.localId
        val counts = db.withTransaction {
            var c = RestoreCounts()
            // Replace: only a group the file has watches for loses its existing ones.
            if (group.watches.isNotEmpty()) {
                val replaced = dao.deleteTargetWatches(TARGET_MOVIE, movieId)
                dao.insertWatches(group.watches.map { WatchEntity(targetType = TARGET_MOVIE, targetId = movieId, watchedAt = it) })
                c = c.copy(historyInserted = group.watches.size, historyReplaced = replaced)
            }
            group.rating?.let {
                dao.upsertRating(RatingEntity(TARGET_MOVIE, movieId, it.value, it.at))
                mirror(SYNC_RATING, TARGET_MOVIE, group.tmdbId, SYNC_SET, it.value, now)
                c = c.copy(ratingsUpserted = 1)
            }
            group.watchlistAt?.let {
                if (dao.insertWatchlist(WatchlistEntity(TARGET_MOVIE, movieId, it)) != -1L) {
                    mirror(SYNC_WATCHLIST, TARGET_MOVIE, group.tmdbId, SYNC_SET, null, now)
                    c = c.copy(watchlistInserted = 1)
                }
            }
            group.favoriteAt?.let {
                if (dao.insertFavorite(FavoriteEntity(TARGET_MOVIE, movieId, it)) != -1L) {
                    mirror(SYNC_FAVORITE, TARGET_MOVIE, group.tmdbId, SYNC_SET, null, now)
                    c = c.copy(favoritesInserted = 1)
                }
            }
            c
        }
        onSyncWrite()
        return counts
    }

    override suspend fun writeRestoreShow(target: RestoreResolved, group: ShowGroup): Pair<RestoreCounts, List<RestoreSkip>> {
        val showId = target.localId
        val resolver = EpisodeResolver(dao.episodeKeys(showId).map { EpisodeKey(it.id, it.seasonNumber, it.episodeNumber, it.tmdbId) })
        val skips = mutableListOf<RestoreSkip>()
        fun missing(kind: String, season: Int, episode: Int) {
            val (hint, last) = episodeSkipHint(resolver.numbers(season), season, episode)
            skips += RestoreSkip(kind, "episode", null, group.tmdbId, season, episode, SKIP_EPISODE_NOT_FOUND, target.title, hint, last)
        }
        val watches = group.watches.mapNotNull { w ->
            val id = resolver.resolve(w.tmdbId, w.season, w.episode)
            if (id == null) missing(RESTORE_HISTORY, w.season, w.episode)
            id?.let { WatchEntity(targetType = TARGET_EPISODE, targetId = it, watchedAt = w.at) }
        }
        val ratings = group.episodeRatings.mapNotNull { r ->
            val id = resolver.resolve(r.tmdbId, r.season, r.episode)
            if (id == null) missing(RESTORE_RATING, r.season, r.episode)
            id?.let { RatingEntity(TARGET_EPISODE, it, r.rating.value, r.rating.at) to r }
        }
        // Read per group: a long import can cross midnight.
        val reading = clock.read()
        val counts = db.withTransaction {
            var c = RestoreCounts()
            // The gate is that the FILE had watches for the show, not that any resolved.
            if (group.watches.isNotEmpty()) {
                val replaced = dao.deleteShowWatches(showId)
                dao.insertWatches(watches)
                c = c.copy(historyInserted = watches.size, historyReplaced = replaced)
            }
            group.showRating?.let {
                dao.upsertRating(RatingEntity(TARGET_SHOW, showId, it.value, it.at))
                mirror(SYNC_RATING, TARGET_SHOW, group.tmdbId, SYNC_SET, it.value, reading.now)
                c = c.copy(ratingsUpserted = c.ratingsUpserted + 1)
            }
            for ((row, planned) in ratings) {
                dao.upsertRating(row)
                mirror(SYNC_RATING, TARGET_EPISODE, group.tmdbId, SYNC_SET, row.rating, reading.now, planned.season, planned.episode)
            }
            c = c.copy(ratingsUpserted = c.ratingsUpserted + ratings.size)
            group.watchlistAt?.let {
                if (dao.insertWatchlist(WatchlistEntity(TARGET_SHOW, showId, it)) != -1L) {
                    mirror(SYNC_WATCHLIST, TARGET_SHOW, group.tmdbId, SYNC_SET, null, reading.now)
                    c = c.copy(watchlistInserted = 1)
                }
            }
            group.favoriteAt?.let {
                if (dao.insertFavorite(FavoriteEntity(TARGET_SHOW, showId, it)) != -1L) {
                    mirror(SYNC_FAVORITE, TARGET_SHOW, group.tmdbId, SYNC_SET, null, reading.now)
                    c = c.copy(favoritesInserted = 1)
                }
            }
            // A rating is not a watch (D-010): only a group with watches recomputes.
            if (group.watches.isNotEmpty()) recompute(showId, reading)
            c
        }
        onSyncWrite()
        return counts to skips
    }

    /** A skip's title, read locally at the end of a run; null when the title is not held. */
    override suspend fun restoreTitle(movie: Boolean, tmdbId: Long): String? =
        if (movie) dao.movieByTmdbId(tmdbId)?.title else dao.showByTmdbId(tmdbId)?.title

    private companion object {
        val STALE_AFTER: Duration = Duration.ofHours(48)
    }
}

private fun EpisodeEntity.ref() = EpisodeRef(id, seasonNumber, episodeNumber, airDate)

private fun StatsTitleRow.toStatsTitle(kind: String) = StatsTitle(
    kind = kind,
    id = id,
    tmdbId = tmdbId,
    title = title,
    year = statsYear(date),
    posterPath = posterPath.clean(),
    backdropPath = backdropPath.clean(),
)

private fun StatsPlayRow.toStatsPlay() = StatsPlay(
    id = id,
    targetType = targetType,
    targetId = targetId,
    watchedAt = watchedAt,
    movieRuntime = movieRuntime,
    movieRelease = movieRelease,
    movieGenres = splitGenres(movieGenres),
    showId = showId,
    season = seasonNumber,
    number = episodeNumber,
    episodeRuntime = episodeRuntime,
    showFirstAir = showFirstAir,
    showGenres = splitGenres(showGenres),
    present = present,
)

private fun ListRow.toEntry() = ListEntry(
    kind = if (targetType == TARGET_MOVIE) ListItemKind.MOVIE else ListItemKind.SHOW,
    localId = localId,
    tmdbId = tmdbId,
    title = title,
    date = date,
    posterPath = posterPath,
    backdropPath = backdropPath,
    score = voteAverage?.takeIf { it > 0 },
    userRating = userRating,
    watched = watched,
    addedAt = addedAt,
)

/** Null only for a row the query's own guard should already have dropped. */
private fun HistoryRow.toItem(episodeScores: Map<Long, Map<Int, Double>>): HistoryItem? = if (targetType == TARGET_MOVIE) {
    movieTmdbId?.let {
        HistoryItem.Movie(
            id, targetId, targetWatches, watchedAt, posterPath, userRating,
            movieScore?.takeIf { s -> s > 0 }, watchlisted, favorited, it, movieTitle.orEmpty(),
        )
    }
} else {
    val season = seasonNumber
    val number = episodeNumber
    if (showId == null || showTmdbId == null || season == null || number == null) {
        null
    } else {
        HistoryItem.Episode(
            id, targetId, targetWatches, watchedAt, posterPath, userRating,
            episodeScores[seasonId]?.get(number), watchlisted, favorited,
            showId, showTmdbId, showTitle.orEmpty(), season, number, episodeTitle,
        )
    }
}

/** TMDB sends "" for an unknown date or text; the database stores null. */
private fun String?.clean(): String? = this?.trim()?.ifEmpty { null }

private fun Fetched<TmdbMovie>.toEntity(id: Long, now: String) = MovieEntity(
    id = id,
    tmdbId = value.id,
    title = value.title,
    releaseDate = value.releaseDate.clean(),
    runtime = value.runtime?.takeIf { it > 0 },
    overview = value.overview.clean(),
    genres = value.genres.joinToString(", ") { it.name }.clean(),
    posterPath = value.posterPath,
    backdropPath = value.backdropPath,
    voteAverage = value.voteAverage?.takeIf { it > 0 },
    metadataJson = body,
    fetchedAt = now,
)

private fun Fetched<TmdbShow>.toEntity(id: Long, now: String) = ShowEntity(
    id = id,
    tmdbId = value.id,
    title = value.name,
    firstAirDate = value.firstAirDate.clean(),
    showStatus = value.status.clean(),
    overview = value.overview.clean(),
    genres = value.genres.joinToString(", ") { it.name }.clean(),
    posterPath = value.posterPath,
    backdropPath = value.backdropPath,
    voteAverage = value.voteAverage?.takeIf { it > 0 },
    metadataJson = body,
    fetchedAt = now,
)

private fun ro.mbyte.sofalog.data.tmdb.TmdbSeasonSummary.toEntity(showId: Long, id: Long) = SeasonEntity(
    id = id,
    showId = showId,
    seasonNumber = seasonNumber,
    name = name.clean(),
    airDate = airDate.clean(),
    overview = overview.clean(),
    posterPath = posterPath,
    episodeCount = episodeCount,
)

private fun ro.mbyte.sofalog.data.tmdb.TmdbEpisode.toEntity(showId: Long, seasonId: Long, seasonNumber: Int, id: Long) =
    EpisodeEntity(
        id = id,
        showId = showId,
        seasonId = seasonId,
        seasonNumber = seasonNumber,
        episodeNumber = episodeNumber,
        tmdbId = this.id,
        title = name.clean(),
        airDate = airDate.clean(),
        runtime = runtime?.takeIf { it > 0 },
        overview = overview.clean(),
        stillPath = stillPath,
        episodeType = episodeType.clean(),
    )
