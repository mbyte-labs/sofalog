package ro.mbyte.sofalog.data

import androidx.room.withTransaction
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import ro.mbyte.sofalog.data.db.AppDatabase
import ro.mbyte.sofalog.data.db.EpisodeEntity
import ro.mbyte.sofalog.data.db.MovieEntity
import ro.mbyte.sofalog.data.db.RatingEntity
import ro.mbyte.sofalog.data.db.SeasonEntity
import ro.mbyte.sofalog.data.db.ShowEntity
import ro.mbyte.sofalog.data.db.ShowStateEntity
import ro.mbyte.sofalog.data.db.TARGET_EPISODE
import ro.mbyte.sofalog.data.db.TARGET_MOVIE
import ro.mbyte.sofalog.data.db.WatchEntity
import ro.mbyte.sofalog.data.tmdb.Fetched
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.data.tmdb.TmdbMovie
import ro.mbyte.sofalog.data.tmdb.TmdbSeason
import ro.mbyte.sofalog.data.tmdb.TmdbShow
import ro.mbyte.sofalog.domain.Clock
import ro.mbyte.sofalog.domain.ClockReading
import ro.mbyte.sofalog.domain.EpisodeRef
import ro.mbyte.sofalog.domain.ShowProgress
import ro.mbyte.sofalog.domain.ShowStatus
import ro.mbyte.sofalog.domain.isEligible
import ro.mbyte.sofalog.domain.recomputeShowStatus
import ro.mbyte.sofalog.domain.showProgress
import ro.mbyte.sofalog.domain.storedInstant
import ro.mbyte.sofalog.domain.EpisodeMarker
import ro.mbyte.sofalog.domain.episodeMarker
import ro.mbyte.sofalog.domain.upNextEpisode
import java.time.Duration
import java.time.Instant

enum class MediaKind { MOVIE, SHOW }

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

/**
 * The single home of the app's reads and writes (SPEC D-040). The UI reads only the local
 * database through this class; TMDB is called for search, for previews of untracked titles and
 * for materialising a title when it is first tracked — never to render something already held.
 *
 * Every write to the watch log recomputes the show's status in the same transaction, and
 * [recompute] is the only writer of `user_show_states`.
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

    /** Logs a watch, materialising the movie first when this is its first reference. */
    suspend fun markMovieWatched(tmdbId: Long) {
        val fetched = if (dao.movieByTmdbId(tmdbId) == null) tmdb.movie(tmdbId) else null
        val reading = clock.read()
        db.withTransaction {
            val movieId = dao.movieByTmdbId(tmdbId)?.id
                ?: dao.insertMovie(checkNotNull(fetched).toEntity(id = 0, now = reading.now))
            dao.insertWatch(WatchEntity(targetType = TARGET_MOVIE, targetId = movieId, watchedAt = reading.now))
        }
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
        val episodes = if (seasonNumber == null) {
            emptyList()
        } else {
            tmdb.season(tmdbId, seasonNumber).value.episodes.map { it.toEntity(0, 0, seasonNumber, 0) }
        }
        return ShowBundle(
            show = root.toEntity(id = 0, now = reading.now),
            seasons = root.value.seasons.map { it.toEntity(showId = 0, id = 0) },
            episodes = episodes,
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
        val seasons = root.value.seasons.map { tmdb.season(tmdbId, it.seasonNumber).value }
        val now = clock.read().now
        return db.withTransaction {
            dao.showByTmdbId(tmdbId)?.id ?: run {
                val showId = dao.insertShow(root.toEntity(id = 0, now = now))
                seasons.forEach { writeSeason(showId, it, root.value) }
                showId
            }
        }
    }

    /**
     * Refreshes a tracked show whose snapshot is older than [STALE_AFTER]: the root, any season
     * it did not have, and its open seasons (the latest, plus any with an undated or future
     * episode). A closed season is not re-fetched. Episodes TMDB no longer lists are kept — they
     * may carry watches. Then the status is recomputed, which is what notices a newly aired
     * episode.
     */
    suspend fun refreshShowIfStale(tmdbId: Long) {
        val show = dao.showByTmdbId(tmdbId) ?: return
        val age = runCatching { Duration.between(Instant.parse(show.fetchedAt), Instant.now()) }.getOrNull()
        if (age != null && age < STALE_AFTER) {
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
        val seasons = wanted.map { tmdb.season(tmdbId, it).value }
        db.withTransaction {
            dao.updateShow(root.toEntity(id = show.id, now = reading.now))
            seasons.forEach { writeSeason(show.id, it, root.value) }
            recompute(show.id, reading)
        }
    }

    /** Upserts one season and its episodes by their natural keys, keeping local ids stable. */
    private suspend fun writeSeason(showId: Long, season: TmdbSeason, root: TmdbShow) {
        val summary = root.seasons.firstOrNull { it.seasonNumber == season.seasonNumber }
        val existing = dao.seasons(showId).firstOrNull { it.seasonNumber == season.seasonNumber }
        val row = SeasonEntity(
            id = existing?.id ?: 0,
            showId = showId,
            seasonNumber = season.seasonNumber,
            name = season.name.clean(),
            airDate = season.airDate.clean(),
            overview = season.overview.clean(),
            posterPath = season.posterPath ?: summary?.posterPath,
            episodeCount = season.episodes.size,
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
     * The bulk mark: every aired, non-special, not-yet-watched episode of the show — or of one
     * season — gets a watch, and the status is recomputed once. Returns how many were marked.
     * The eligibility is D-012's predicate, the same one the recompute uses.
     */
    suspend fun markAiredWatched(showTmdbId: Long, season: Int?): Int {
        val showId = trackShow(showTmdbId)
        val reading = clock.read()
        return db.withTransaction {
            val watched = dao.watchedEpisodeIds(showId).toSet()
            val targets = dao.episodes(showId).filter {
                (season == null || it.seasonNumber == season) &&
                    it.ref().isEligible(reading.today) && it.id !in watched
            }
            dao.insertWatches(
                targets.map { WatchEntity(targetType = TARGET_EPISODE, targetId = it.id, watchedAt = reading.now) },
            )
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

    private companion object {
        val STALE_AFTER: Duration = Duration.ofHours(48)
    }
}

private fun EpisodeEntity.ref() = EpisodeRef(id, seasonNumber, episodeNumber, airDate)

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
