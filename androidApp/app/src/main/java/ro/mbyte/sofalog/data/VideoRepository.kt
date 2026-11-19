package ro.mbyte.sofalog.data

import androidx.room.withTransaction
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import ro.mbyte.sofalog.data.db.AppDatabase
import ro.mbyte.sofalog.data.db.CopyEntity
import ro.mbyte.sofalog.data.db.FavoriteEntity
import ro.mbyte.sofalog.data.db.WatchlistEntity
import ro.mbyte.sofalog.data.db.HistoryRow
import ro.mbyte.sofalog.data.db.EpisodeEntity
import ro.mbyte.sofalog.data.db.MovieEntity
import ro.mbyte.sofalog.data.db.RatingEntity
import ro.mbyte.sofalog.data.db.SeasonEntity
import ro.mbyte.sofalog.data.db.ShowEntity
import ro.mbyte.sofalog.data.db.ShowStateEntity
import ro.mbyte.sofalog.data.db.TARGET_EPISODE
import ro.mbyte.sofalog.data.db.TARGET_MOVIE
import ro.mbyte.sofalog.data.db.TARGET_SEASON
import ro.mbyte.sofalog.data.db.TARGET_SHOW
import ro.mbyte.sofalog.data.db.WatchEntity
import ro.mbyte.sofalog.data.tmdb.Fetched
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.data.tmdb.TmdbMovie
import ro.mbyte.sofalog.data.tmdb.TmdbSeason
import ro.mbyte.sofalog.data.tmdb.TmdbShow
import ro.mbyte.sofalog.domain.BulkWatchMode
import ro.mbyte.sofalog.domain.Clock
import ro.mbyte.sofalog.domain.ClockReading
import ro.mbyte.sofalog.domain.EpisodeRef
import ro.mbyte.sofalog.domain.ShowProgress
import ro.mbyte.sofalog.domain.ShowStatus
import ro.mbyte.sofalog.domain.isEligible
import ro.mbyte.sofalog.domain.movieSnapshotIncomplete
import ro.mbyte.sofalog.domain.bulkWatchInstant
import ro.mbyte.sofalog.domain.recomputeShowStatus
import ro.mbyte.sofalog.domain.seasonSnapshotIncomplete
import ro.mbyte.sofalog.domain.showSnapshotIncomplete
import ro.mbyte.sofalog.domain.showProgress
import ro.mbyte.sofalog.domain.storedInstant
import ro.mbyte.sofalog.domain.EpisodeMarker
import ro.mbyte.sofalog.domain.episodeMarker
import ro.mbyte.sofalog.domain.upNextEpisode
import java.time.Duration
import java.time.Instant
import java.time.ZoneId

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
    /** TMDB's community score. Null for an episode: the device stores no season score. */
    val score: Double?

    data class Movie(
        override val id: Long,
        override val targetId: Long,
        override val entries: Int,
        override val watchedAt: String,
        override val posterPath: String?,
        override val userRating: Int?,
        override val score: Double?,
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
        /** The show's local id — what the season- and show-wide removals address. */
        val showId: Long,
        val showTmdbId: Long,
        val showTitle: String,
        val season: Int,
        val number: Int,
        val title: String?,
    ) : HistoryItem {
        override val score: Double? get() = null
    }
}

data class HistoryPage(val items: List<HistoryItem>, val hasMore: Boolean)

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
) {
    private val dao = db.videoDao()

    val hasTmdbKey: Boolean get() = tmdb.hasKey

    // ------------------------------------------------------------------ search

    suspend fun search(query: String): List<SearchHit> = tmdb.search(query).map {
        val movie = it.mediaType == "movie"
        SearchHit(
            kind = if (movie) MediaKind.MOVIE else MediaKind.SHOW,
            tmdbId = it.id,
            title = (if (movie) it.title else it.name).orEmpty(),
            year = (if (movie) it.releaseDate else it.firstAirDate).clean()?.take(4),
            posterPath = it.posterPath,
            voteAverage = it.voteAverage,
        )
    }

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
            rating?.let { dao.upsertRating(RatingEntity(TARGET_MOVIE, movieId, it, reading.now)) }
        }
    }

    /** Sets or — with null — clears the rating. Rating is a tracking action; clearing is not. */
    suspend fun rateMovie(tmdbId: Long, rating: Int?) {
        require(rating == null || rating in 1..10) { "rating must be 1..10" }
        if (rating == null) {
            removeFromMovie(tmdbId) { dao.deleteRating(TARGET_MOVIE, it) }
        } else {
            addToMovie(tmdbId) { id, now -> dao.upsertRating(RatingEntity(TARGET_MOVIE, id, rating, now)) }
        }
    }

    suspend fun setMovieWatchlisted(tmdbId: Long, on: Boolean) = if (on) {
        addToMovie(tmdbId) { id, now -> dao.insertWatchlist(WatchlistEntity(TARGET_MOVIE, id, now)) }
    } else {
        removeFromMovie(tmdbId) { dao.deleteWatchlist(TARGET_MOVIE, it) }
    }

    suspend fun setMovieFavorited(tmdbId: Long, on: Boolean) = if (on) {
        addToMovie(tmdbId) { id, now -> dao.insertFavorite(FavoriteEntity(TARGET_MOVIE, id, now)) }
    } else {
        removeFromMovie(tmdbId) { dao.deleteFavorite(TARGET_MOVIE, it) }
    }

    suspend fun setMovieCopy(tmdbId: Long, kind: CopyKind, owned: Boolean) = if (owned) {
        addToMovie(tmdbId) { id, now -> dao.insertCopy(CopyEntity(TARGET_MOVIE, id, kind.value, now)) }
    } else {
        removeFromMovie(tmdbId) { dao.deleteCopy(TARGET_MOVIE, it, kind.value) }
    }

    private suspend fun addToMovie(tmdbId: Long, write: suspend (movieId: Long, now: String) -> Unit) {
        val ensure = movieGate(tmdbId)
        val now = clock.read().now
        db.withTransaction { write(ensure(now), now) }
    }

    /** A removal never fetches: an untracked movie has nothing to remove. The catalog row stays. */
    private suspend fun removeFromMovie(tmdbId: Long, write: suspend (movieId: Long) -> Unit) {
        db.withTransaction { dao.movieByTmdbId(tmdbId)?.let { write(it.id) } }
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
            removeFromShow(tmdbId) { dao.deleteRating(TARGET_SHOW, it) }
        } else {
            addToShow(tmdbId) { id, now -> dao.upsertRating(RatingEntity(TARGET_SHOW, id, rating, now)) }
        }
    }

    /**
     * Sets or — with null — clears one episode's rating without logging a watch. Rating is a
     * tracking action, so an untracked show is fetched first; clearing never fetches.
     */
    suspend fun rateEpisode(showTmdbId: Long, seasonNumber: Int, number: Int, rating: Int?) {
        require(rating == null || rating in 1..10) { "rating must be 1..10" }
        if (rating == null) {
            removeFromShow(showTmdbId) { id ->
                dao.episodeByKey(id, seasonNumber, number)?.let { dao.deleteRating(TARGET_EPISODE, it.id) }
            }
        } else {
            addToShow(showTmdbId) { id, now ->
                val episode = dao.episodeByKey(id, seasonNumber, number)
                    ?: throw NoSuchElementException("episode $seasonNumber x $number")
                dao.upsertRating(RatingEntity(TARGET_EPISODE, episode.id, rating, now))
            }
        }
    }

    suspend fun setShowWatchlisted(tmdbId: Long, on: Boolean) = if (on) {
        addToShow(tmdbId) { id, now -> dao.insertWatchlist(WatchlistEntity(TARGET_SHOW, id, now)) }
    } else {
        removeFromShow(tmdbId) { dao.deleteWatchlist(TARGET_SHOW, it) }
    }

    suspend fun setShowFavorited(tmdbId: Long, on: Boolean) = if (on) {
        addToShow(tmdbId) { id, now -> dao.insertFavorite(FavoriteEntity(TARGET_SHOW, id, now)) }
    } else {
        removeFromShow(tmdbId) { dao.deleteFavorite(TARGET_SHOW, it) }
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
        removeFromShow(tmdbId) { id ->
            dao.seasons(id).filter { it.seasonNumber > 0 }
                .forEach { dao.deleteCopy(TARGET_SEASON, it.id, kind.value) }
        }
    }

    /** An add tracks the show first ([trackShow] fetches outside the transaction). */
    private suspend fun addToShow(tmdbId: Long, write: suspend (showId: Long, now: String) -> Unit) {
        val showId = trackShow(tmdbId)
        val now = clock.read().now
        db.withTransaction { write(showId, now) }
    }

    /** A removal never fetches: an untracked show has nothing to remove. The catalog rows stay. */
    private suspend fun removeFromShow(tmdbId: Long, write: suspend (showId: Long) -> Unit) {
        db.withTransaction { dao.showByTmdbId(tmdbId)?.let { write(it.id) } }
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
        removeFromShow(tmdbId) { id ->
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
            rating?.let { dao.upsertRating(RatingEntity(TARGET_EPISODE, episode.id, it, reading.now)) }
            recompute(showId, reading)
        }
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
        val zone = ZoneId.systemDefault()
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
        combine(dao.observeStartedShows(), dao.observeShowStates()) { started, states ->
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
            HistoryPage(list.take(limit).mapNotNull { it.toItem() }, hasMore = list.size > limit)
        }
    }

    private companion object {
        val STALE_AFTER: Duration = Duration.ofHours(48)
    }
}

private fun EpisodeEntity.ref() = EpisodeRef(id, seasonNumber, episodeNumber, airDate)

/** Null only for a row the query's own guard should already have dropped. */
private fun HistoryRow.toItem(): HistoryItem? = if (targetType == TARGET_MOVIE) {
    movieTmdbId?.let {
        HistoryItem.Movie(
            id, targetId, targetWatches, watchedAt, posterPath, userRating,
            movieScore?.takeIf { s -> s > 0 }, it, movieTitle.orEmpty(),
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
