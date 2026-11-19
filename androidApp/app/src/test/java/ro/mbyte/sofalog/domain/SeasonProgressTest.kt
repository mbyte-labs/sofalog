package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class SeasonProgressTest {
    private val today = "2026-11-19"
    private val episodes = listOf(
        EpisodeRef(1, 0, 1, "2020-01-01"),
        EpisodeRef(2, 1, 1, "2021-01-01"),
        EpisodeRef(3, 1, 2, "2021-01-08"),
        EpisodeRef(4, 1, 3, today),
        EpisodeRef(5, 2, 1, "2027-01-01"),
        EpisodeRef(6, 2, 2, null),
    )

    @Test
    fun countsEachSeasonOnItsOwn() {
        val progress = seasonProgress(episodes, watched = setOf(2L, 4L, 5L), today = today)
        // An episode airing today has aired; a watched but unaired one is not counted as progress.
        assertEquals(SeasonProgress(watched = 2, airedTotal = 3, total = 3), progress[1])
        assertEquals(SeasonProgress(watched = 0, airedTotal = 0, total = 2), progress[2])
    }

    @Test
    fun specialsCountTheirOwnEpisodes() {
        // Display, not completion: the show's own progress leaves season 0 out, the grid does not.
        val progress = seasonProgress(episodes, watched = setOf(1L), today = today)
        assertEquals(SeasonProgress(watched = 1, airedTotal = 1, total = 1), progress[0])
        assertEquals(ShowProgress(watched = 0, airedTotal = 3), showProgress(episodes, setOf(1L), today))
    }

    @Test
    fun ratingMeanIsNullUntilSomethingIsRated() {
        assertNull(episodeRatingMean(emptyList()))
        assertEquals(7.5, episodeRatingMean(listOf(7, 8))!!, 0.0)
        assertEquals(8.0, episodeRatingMean(listOf(10, 9, 5))!!, 0.0)
    }
}
