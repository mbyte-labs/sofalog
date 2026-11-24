package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Test

class SearchMarksTest {
    @Test
    fun aMovieAndAShowSharingAnIdKeepTheirOwnMarks() {
        val marks = LibraryMarks(
            movies = mapOf(5L to LibraryMark(userRating = 8, watched = true, favorited = true)),
            shows = mapOf(5L to LibraryMark(watchlisted = true, dropped = true)),
        )
        assertEquals(LibraryMark(8, watched = true, watchlisted = false, favorited = true), marks.of(movie = true, tmdbId = 5))
        assertEquals(LibraryMark(null, watched = false, watchlisted = true, favorited = false, dropped = true), marks.of(movie = false, tmdbId = 5))
    }

    @Test
    fun aTitleTheLibraryDoesNotHoldIsUnmarked() {
        val marks = LibraryMarks(movies = mapOf(5L to LibraryMark(8, true)), shows = emptyMap())
        assertEquals(LibraryMark(), marks.of(movie = true, tmdbId = 6))
        assertEquals(LibraryMark(), marks.of(movie = false, tmdbId = 5))
        assertEquals(LibraryMark(), LibraryMarks.EMPTY.of(movie = true, tmdbId = 5))
    }
}
