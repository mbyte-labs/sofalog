package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DiscoverTest {
    @Test
    fun anUnfilteredQuerySendsOnlyTheSortAndThePage() {
        assertEquals(
            listOf("sort_by" to "popularity.desc", "include_adult" to "false", "page" to "1"),
            discoverParams(tv = false, DiscoverFilters(voteMin = null), page = 1),
        )
    }

    @Test
    fun theDefaultFiltersStartAtTheDefaultScore() {
        assertEquals(
            listOf("sort_by" to "popularity.desc", "include_adult" to "false", "page" to "1", "vote_average.gte" to "5.5"),
            discoverParams(tv = false, DiscoverFilters(), page = 1),
        )
    }

    @Test
    fun aShowQueryLeavesUndatedShowsOut() {
        assertEquals(
            listOf(
                "sort_by" to "popularity.desc",
                "include_adult" to "false",
                "include_null_first_air_dates" to "false",
                "page" to "3",
            ),
            discoverParams(tv = true, DiscoverFilters(voteMin = null), page = 3),
        )
    }

    @Test
    fun everyFilterBecomesItsTmdbParameter() {
        val params = discoverParams(
            tv = false,
            DiscoverFilters(language = "ro", genres = setOf(35L, 18L), voteMin = 6.0),
            page = 2,
        ).toMap()
        assertEquals("6", params["vote_average.gte"])
        assertEquals("18,35", params["with_genres"])
        assertEquals("ro", params["with_original_language"])
        assertEquals("2", params["page"])
    }

    @Test
    fun theBadgeCountsWhatTheUserChanged() {
        assertEquals(0, DiscoverFilters().activeCount)
        assertEquals(1, DiscoverFilters(voteMin = 6.0).activeCount)
        assertEquals(1, DiscoverFilters(voteMin = null).activeCount)
        assertEquals(3, DiscoverFilters(language = "en", genres = setOf(1L), voteMin = 6.0).activeCount)
    }

    @Test
    fun aTypedTrendingListIgnoresTheRowsOwnKind() {
        assertEquals("movie", trendingRowKind("movie", null, 5))
        assertEquals("tv", trendingRowKind("tv", "movie", 5))
    }

    @Test
    fun theMixedTrendingListDropsPeopleAndIdlessRows() {
        assertEquals("movie", trendingRowKind("all", "movie", 5))
        assertEquals("tv", trendingRowKind("all", "tv", 5))
        assertNull(trendingRowKind("all", "person", 5))
        assertNull(trendingRowKind("all", null, 5))
        assertNull(trendingRowKind("movie", "movie", 0))
    }

    @Test
    fun theMergeIsByPopularityThenTitleThenId() {
        val rows = listOf(
            RankedRow("show-b", 50.0, "beta", 9),
            RankedRow("movie-top", 90.0, "Zed", 1),
            RankedRow("movie-a", 50.0, "Alpha", 7),
            RankedRow("show-b-twin", 50.0, "Beta", 4),
        )
        assertEquals(listOf("movie-top", "movie-a", "show-b-twin", "show-b"), mergeByPopularity(rows))
    }

    @Test
    fun languagesAreSortedByNameWithoutTheEmptyOnes() {
        val options = languageOptions(
            listOf("ro" to "Romanian", "xx" to "No Language", "en" to "English", "" to "Nothing", "zz" to ""),
        )
        assertEquals(listOf(LanguageOption("en", "English"), LanguageOption("ro", "Romanian")), options)
    }

    @Test
    fun genresFollowTheKindAndBothIsTheUnion() {
        val movie = listOf(GenreOption(28, "Action"), GenreOption(18, "Drama"), GenreOption(0, "Broken"))
        val show = listOf(GenreOption(18, "Drama"), GenreOption(10759, "Action & Adventure"), GenreOption(7, ""))
        assertEquals(listOf(GenreOption(28, "Action"), GenreOption(18, "Drama")), genreOptions(true, movie, show))
        assertEquals(listOf(GenreOption(18, "Drama"), GenreOption(10759, "Action & Adventure")), genreOptions(false, movie, show))
        assertEquals(
            listOf(GenreOption(28, "Action"), GenreOption(10759, "Action & Adventure"), GenreOption(18, "Drama")),
            genreOptions(null, movie, show),
        )
    }

    @Test
    fun moreExistsUntilTheLastPageOrTmdbsCeiling() {
        assertTrue(discoverHasMore(page = 1, totalPages = 3))
        assertFalse(discoverHasMore(page = 3, totalPages = 3))
        assertFalse(discoverHasMore(page = 500, totalPages = 900))
    }
}
