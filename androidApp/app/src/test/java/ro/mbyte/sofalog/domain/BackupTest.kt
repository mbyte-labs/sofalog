package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.time.Instant

/** The app's own backup file: what is written, what is read back, and what a restore makes of it. */
class BackupTest {
    private val now = Instant.parse("2026-11-23T10:00:00Z")
    private val kinds = setOf("dvd", "bluray", "digital")

    private val rows = BackupRows(
        movieWatches = listOf(
            BackupMovieWatchRow(603, "The Matrix", "2025-03-01T20:00:00Z"),
            BackupMovieWatchRow(603, "The Matrix", "2024-01-02T20:00:00Z"),
        ),
        episodeWatches = listOf(
            BackupEpisodeWatchRow(1396, "Breaking Bad", 1, 2, 62086, "2024-02-02T20:00:00Z"),
            BackupEpisodeWatchRow(1396, "Breaking Bad", 1, 1, 62085, "2024-02-01T20:00:00Z"),
            BackupEpisodeWatchRow(1396, "Breaking Bad", 1, 1, 62085, "2025-02-01T20:00:00Z"),
        ),
        ratings = listOf(
            BackupRatingRow("movie", 603, "The Matrix", 9, "2024-01-02T21:00:00Z"),
            BackupRatingRow("show", 1396, "Breaking Bad", 10, "2024-03-01T21:00:00Z"),
        ),
        episodeRatings = listOf(BackupEpisodeRatingRow(1396, "Breaking Bad", 2, 1, null, 8, "2024-03-02T21:00:00Z")),
        watchlist = listOf(BackupListRow("movie", 27205, "Inception", "2024-05-01T10:00:00Z")),
        favorites = listOf(BackupListRow("show", 1396, "Breaking Bad", "2024-05-02T10:00:00Z")),
        dropped = listOf(BackupListRow("show", 1399, "Game of Thrones", "2024-06-01T10:00:00Z")),
        movieCopies = listOf(BackupMovieCopyRow(603, "The Matrix", "bluray", "2024-07-01T10:00:00Z")),
        seasonCopies = listOf(BackupSeasonCopyRow(1396, "Breaking Bad", 1, "dvd", "2024-07-02T10:00:00Z")),
    )

    private fun backup() = buildBackup(rows, "2026-11-23T09:00:00Z", "1.0")

    private fun encode(backup: SofalogBackup): String =
        ByteArrayOutputStream().also { writeSofalogBackup(backup, it) }.toString(Charsets.UTF_8)

    private fun parse(text: String) = parseSofalogBackup { text.byteInputStream() }

    private fun rejected(text: String) = parse(text) as BackupParse.Rejected

    @Test
    fun rowsFoldIntoTitlesInAFixedOrder() {
        val b = backup()
        assertEquals(BACKUP_FORMAT, b.format)
        assertEquals(BACKUP_SCHEMA_VERSION, b.schemaVersion)
        assertEquals(listOf(603L, 27205L), b.movies.map { it.tmdbId })
        assertEquals(listOf(1396L, 1399L), b.shows.map { it.tmdbId })

        val matrix = b.movies[0]
        assertEquals(listOf("2024-01-02T20:00:00Z", "2025-03-01T20:00:00Z"), matrix.watches)
        assertEquals(BackupRating(9, "2024-01-02T21:00:00Z"), matrix.rating)
        assertEquals(listOf(BackupCopy("bluray", "2024-07-01T10:00:00Z")), matrix.copies)
        // Watchlisted and nothing else: still a title of the file.
        assertEquals("2024-05-01T10:00:00Z", b.movies[1].watchlistAt)
        assertTrue(b.movies[1].watches.isEmpty())

        val show = b.shows[0]
        assertEquals(listOf(1 to 1, 1 to 2, 2 to 1), show.episodes.map { it.season to it.episode })
        assertEquals(2, show.episodes[0].watches.size)
        assertEquals(62085L, show.episodes[0].tmdbId)
        // Rated, never watched: the episode is listed for its rating alone.
        assertTrue(show.episodes[2].watches.isEmpty())
        assertEquals(8, show.episodes[2].rating?.value)
        assertEquals("2024-05-02T10:00:00Z", show.favoriteAt)
        assertEquals(listOf(BackupSeasonCopy(1, "dvd", "2024-07-02T10:00:00Z")), show.seasonCopies)
        // A dropped show with nothing else of the user's on it.
        assertEquals("2024-06-01T10:00:00Z", b.shows[1].droppedAt)
    }

    @Test
    fun theSummaryCountsEveryEntry() {
        assertEquals(
            BackupSummary(movies = 2, shows = 2, watches = 5, ratings = 3, watchlist = 1, favorites = 1, dropped = 1, copies = 2),
            backup().summary(),
        )
    }

    @Test
    fun theFileLeavesOutWhatIsNotThere() {
        val text = encode(backup())
        assertFalse(text.contains("null"))
        assertFalse(text.contains("\"watches\":[]"))
        assertTrue(text.startsWith("{\"format\":\"sofalog-backup\",\"schemaVersion\":1,"))
    }

    @Test
    fun aWrittenFileReadsBackAsItself() {
        val b = backup()
        assertEquals(BackupParse.Ok(b), parse(encode(b)))
    }

    @Test
    fun anEmptyLibraryIsStillABackup() {
        val empty = buildBackup(BackupRows(), "2026-11-23T09:00:00Z", "1.0")
        assertEquals(BackupParse.Ok(empty), parse(encode(empty)))
        assertEquals(0, validateSofalogBackup(empty, now, kinds).first.groups)
    }

    @Test
    fun otherFilesAreRefusedWithAReason() {
        assertEquals("not_json", rejected("not json").code)
        // A Ticket Box export has a schemaVersion and no format.
        assertEquals("format", rejected("""{"schemaVersion":5,"history":[]}""").code)
        assertEquals("format", rejected("""{"format":7,"schemaVersion":1}""").code)
        val newer = rejected("""{"format":"sofalog-backup","schemaVersion":2}""")
        assertEquals("version", newer.code)
        assertTrue(newer.message.contains("newer"))
        assertEquals("version", rejected("""{"format":"sofalog-backup","schemaVersion":"1"}""").code)
        val shape = rejected("""{"format":"sofalog-backup","schemaVersion":1,"books":[]}""")
        assertEquals("shape", shape.code)
        assertTrue(shape.message.contains("\"books\""))
    }

    @Test
    fun aBackupBecomesExactGroupsThatMatchItsRows() {
        val (plan, skips) = validateSofalogBackup(backup(), now, kinds)
        assertTrue(skips.isEmpty())
        assertTrue(plan.movies.all { it.exact } && plan.shows.all { it.exact })

        val matrix = plan.movies.first { it.tmdbId == 603L }
        assertEquals(listOf("2024-01-02T20:00:00Z", "2025-03-01T20:00:00Z"), matrix.watches)
        assertEquals(PlannedRating(9, "2024-01-02T21:00:00Z"), matrix.rating)
        assertEquals(listOf(PlannedCopy("bluray", "2024-07-01T10:00:00Z")), matrix.copies)
        assertEquals("2024-05-01T10:00:00Z", plan.movies.first { it.tmdbId == 27205L }.watchlistAt)

        val show = plan.shows.first { it.tmdbId == 1396L }
        assertEquals(
            listOf(
                PlannedEpisodeWatch(1, 1, 62085, "2024-02-01T20:00:00Z"),
                PlannedEpisodeWatch(1, 1, 62085, "2025-02-01T20:00:00Z"),
                PlannedEpisodeWatch(1, 2, 62086, "2024-02-02T20:00:00Z"),
            ),
            show.watches,
        )
        assertEquals(PlannedRating(10, "2024-03-01T21:00:00Z"), show.showRating)
        assertEquals(listOf(PlannedEpisodeRating(2, 1, null, PlannedRating(8, "2024-03-02T21:00:00Z"))), show.episodeRatings)
        assertEquals("2024-05-02T10:00:00Z", show.favoriteAt)
        assertEquals(listOf(PlannedSeasonCopy(1, "dvd", "2024-07-02T10:00:00Z")), show.seasonCopies)
        assertNull(show.droppedAt)
        assertEquals("2024-06-01T10:00:00Z", plan.shows.first { it.tmdbId == 1399L }.droppedAt)

        // The plan carries exactly what the file counted.
        val s = backup().summary()
        assertEquals(s.watches + s.ratings + s.watchlist + s.favorites + s.dropped + s.copies, plan.movies.sumOf { it.entries } + plan.shows.sumOf { it.entries })
    }

    @Test
    fun badEntriesAreSkippedOneByOne() {
        val file = SofalogBackup(
            format = BACKUP_FORMAT,
            schemaVersion = 1,
            movies = listOf(
                BackupMovie(tmdbId = null, title = "No id", watches = listOf("2024-01-01T00:00:00Z")),
                BackupMovie(
                    tmdbId = 1,
                    watches = listOf("2024-01-01T00:00:00+02:00", "yesterday", "2030-01-01T00:00:00Z"),
                    rating = BackupRating(11, "2024-01-01T00:00:00Z"),
                    copies = listOf(BackupCopy("laserdisc", "2024-01-01T00:00:00Z"), BackupCopy("dvd", "2024-01-01T00:00:00Z"), BackupCopy("dvd", "2024-02-01T00:00:00Z")),
                ),
                // Every entry refused: no group, so nothing on the device is replaced for it.
                BackupMovie(tmdbId = 2, watches = listOf("nonsense")),
            ),
            shows = listOf(
                BackupShow(
                    tmdbId = 10,
                    episodes = listOf(
                        BackupEpisode(season = 1, episode = null, watches = listOf("2024-01-01T00:00:00Z")),
                        BackupEpisode(season = 0, episode = 0, watches = listOf("2024-01-01T00:00:00Z"), rating = BackupRating(0, "2024-01-01T00:00:00Z")),
                    ),
                    seasonCopies = listOf(BackupSeasonCopy(null, "dvd", "2024-01-01T00:00:00Z"), BackupSeasonCopy(1, "vhs", "2024-01-01T00:00:00Z")),
                    droppedAt = "2031-01-01T00:00:00Z",
                ),
            ),
        )
        val (plan, skips) = validateSofalogBackup(file, now, kinds)

        assertEquals(listOf(1L), plan.movies.map { it.tmdbId })
        val movie = plan.movies.single()
        // The offset is folded into UTC.
        assertEquals(listOf("2023-12-31T22:00:00Z"), movie.watches)
        assertNull(movie.rating)
        assertEquals(listOf(PlannedCopy("dvd", "2024-01-01T00:00:00Z")), movie.copies)

        val show = plan.shows.single()
        // Specials and an episode 0 are positions like any other.
        assertEquals(listOf(PlannedEpisodeWatch(0, 0, null, "2024-01-01T00:00:00Z")), show.watches)
        assertTrue(show.episodeRatings.isEmpty() && show.seasonCopies.isEmpty())
        assertNull(show.droppedAt)

        assertEquals(
            listOf(
                RESTORE_TITLE to SKIP_MISSING_TMDB_ID,
                RESTORE_HISTORY to SKIP_INVALID_TIMESTAMP,
                RESTORE_HISTORY to SKIP_FUTURE_TIMESTAMP,
                RESTORE_RATING to SKIP_RATING_OUT_OF_RANGE,
                RESTORE_COPY to SKIP_UNKNOWN_COPY_KIND,
                RESTORE_HISTORY to SKIP_INVALID_TIMESTAMP,
                RESTORE_HISTORY to SKIP_MISSING_POSITION,
                RESTORE_RATING to SKIP_RATING_OUT_OF_RANGE,
                RESTORE_COPY to SKIP_MISSING_POSITION,
                RESTORE_COPY to SKIP_UNKNOWN_COPY_KIND,
                RESTORE_DROPPED to SKIP_FUTURE_TIMESTAMP,
            ),
            skips.map { it.kind to it.reason },
        )
        assertEquals("No id", skips.first().title)
    }

    @Test
    fun aWholeGroupRefusalCountsCopiesAndTheDrop() {
        val group = ShowGroup(
            10, emptyList(), null, emptyList(), null, null,
            seasonCopies = listOf(PlannedSeasonCopy(1, "dvd", "2024-01-01T00:00:00Z")),
            droppedAt = "2024-01-01T00:00:00Z",
            exact = true,
        )
        assertEquals(listOf(RESTORE_COPY, RESTORE_DROPPED), group.skips(SKIP_SHOW_NOT_FOUND).map { it.kind })
        assertEquals("Copy", skipKindLabel(RESTORE_COPY))
        assertEquals("Dropped", skipKindLabel(RESTORE_DROPPED))
    }

    @Test
    fun theFileIsNamedForTheDay() {
        assertEquals("sofalog-backup-2026-11-23.json", backupFileName("2026-11-23"))
    }
}
