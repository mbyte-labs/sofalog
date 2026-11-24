package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Test

class ListOrderTest {
    private fun entry(
        kind: ListItemKind,
        id: Long,
        title: String = "T$id",
        date: String? = "2020-01-01",
        addedAt: String = "2026-01-01T00:00:00Z",
        poster: String? = null,
    ) = ListEntry(kind, id, id + 1000, title, date, poster, null, null, null, false, addedAt)

    private fun movie(id: Long, title: String = "T$id", date: String? = "2020-01-01", addedAt: String = "2026-01-01T00:00:00Z") =
        entry(ListItemKind.MOVIE, id, title, date, addedAt)

    private fun show(id: Long, title: String = "T$id", date: String? = "2020-01-01", addedAt: String = "2026-01-01T00:00:00Z") =
        entry(ListItemKind.SHOW, id, title, date, addedAt)

    private fun keys(list: List<ListEntry>) = list.map { "${it.kind.name.first()}${it.localId}" }

    @Test
    fun theDefaultIsNewestAddedFirst() {
        val list = listOf(
            movie(1, addedAt = "2026-03-01T10:00:00Z"),
            show(2, addedAt = "2026-05-01T10:00:00Z"),
            movie(3, addedAt = "2026-04-01T10:00:00Z"),
        )
        assertEquals(listOf("S2", "M3", "M1"), keys(orderList(list, ListQuery())))
        assertEquals(listOf("M1", "M3", "S2"), keys(orderList(list, ListQuery(descending = false))))
    }

    @Test
    fun aTieBreaksOnThePairInTheKeysDirection() {
        // Movie 5 and show 5 both exist: the id alone orders nothing.
        val list = listOf(show(5), movie(7), movie(5), show(3))
        assertEquals(listOf("M5", "M7", "S3", "S5"), keys(orderList(list, ListQuery(descending = false))))
        assertEquals(listOf("S5", "S3", "M7", "M5"), keys(orderList(list, ListQuery())))
    }

    @Test
    fun titlesCompareCaseFolded() {
        val list = listOf(movie(1, "zodiac"), show(2, "Andor"), movie(3, "black Mirror"), show(4, "Arrival"))
        val asc = ListQuery(sort = ListSort.TITLE, descending = false)
        assertEquals(listOf("S2", "S4", "M3", "M1"), keys(orderList(list, asc)))
        assertEquals(listOf("M1", "M3", "S4", "S2"), keys(orderList(list, asc.copy(descending = true))))
    }

    @Test
    fun undatedEntriesSortLastBothWays() {
        val list = listOf(movie(1, date = null), show(2, date = "2019-06-01"), movie(3, date = "2024-02-02"), show(4, date = null))
        val asc = ListQuery(sort = ListSort.RELEASE, descending = false)
        assertEquals(listOf("S2", "M3", "M1", "S4"), keys(orderList(list, asc)))
        assertEquals(listOf("M3", "S2", "S4", "M1"), keys(orderList(list, asc.copy(descending = true))))
    }

    @Test
    fun theKindFilterKeepsOneKind() {
        val list = listOf(movie(1), show(2), movie(3))
        assertEquals(listOf("M1", "M3"), keys(orderList(list, ListQuery(kind = ListItemKind.MOVIE, descending = false))))
        assertEquals(listOf("S2"), keys(orderList(list, ListQuery(kind = ListItemKind.SHOW))))
    }

    @Test
    fun anEmptyListSummarizesToNothing() {
        assertEquals(ListSummary(0, 0, emptyList()), summarize(emptyList()))
    }

    @Test
    fun theSummaryCountsKindsAndPicksTheNewestPosters() {
        val list = listOf(
            entry(ListItemKind.MOVIE, 1, addedAt = "2026-01-01T00:00:00Z", poster = "/a"),
            entry(ListItemKind.SHOW, 2, addedAt = "2026-01-05T00:00:00Z", poster = null),
            entry(ListItemKind.MOVIE, 3, addedAt = "2026-01-04T00:00:00Z", poster = "/c"),
            entry(ListItemKind.SHOW, 4, addedAt = "2026-01-03T00:00:00Z", poster = "/d"),
            entry(ListItemKind.SHOW, 5, addedAt = "2026-01-02T00:00:00Z", poster = "/e"),
            entry(ListItemKind.MOVIE, 6, addedAt = "2026-01-06T00:00:00Z", poster = "/f"),
            entry(ListItemKind.SHOW, 7, addedAt = "2025-12-31T00:00:00Z", poster = "/g"),
            entry(ListItemKind.MOVIE, 8, addedAt = "2025-12-30T00:00:00Z", poster = "/h"),
        )
        val summary = summarize(list)
        assertEquals(4, summary.movies)
        assertEquals(4, summary.shows)
        assertEquals(8, summary.total)
        // Newest first, the entry without artwork skipped, five at most.
        assertEquals(listOf("/f", "/c", "/d", "/e", "/a"), summary.posters)
    }

    @Test
    fun artworkComesFromTheNewestDozenOnly() {
        // Twelve newer entries without a poster hide the one older entry that has one.
        val bare = (1L..12L).map { entry(ListItemKind.MOVIE, it, addedAt = "2026-02-%02dT00:00:00Z".format(it)) }
        val old = entry(ListItemKind.MOVIE, 99, addedAt = "2025-01-01T00:00:00Z", poster = "/old")
        assertEquals(emptyList<String>(), summarize(bare + old).posters)
        assertEquals(listOf("/old"), summarize(bare.drop(1) + old).posters)
    }
}
