package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MovieSnapshotTest {
    private fun provider(id: Int, name: String, priority: Int = 0) =
        """{"provider_id":$id,"provider_name":"$name","logo_path":"/$id.jpg","display_priority":$priority}"""

    private val body = """
        {
          "id": 603, "title": "The Matrix", "tagline": "Free your mind.", "homepage": "",
          "imdb_id": "tt0133093", "original_language": "en", "budget": 63000000, "revenue": 0,
          "popularity": 81.25, "vote_count": 26000,
          "credits": {
            "cast": [
              {"name": "Carrie-Anne Moss", "character": "Trinity", "profile_path": null, "order": 2},
              {"name": "Keanu Reeves", "character": "Neo", "profile_path": "/k.jpg", "order": 0},
              {"name": "", "character": "Nobody", "order": 1}
            ],
            "crew": [{"name": "Someone", "job": "Producer"}, {"name": "Lana Wachowski", "job": "Director"}]
          },
          "external_ids": {"imdb_id": "tt0133093"},
          "videos": {"results": [
            {"key": "teaser0001", "name": "Teaser", "site": "YouTube", "type": "Teaser", "official": true, "published_at": "2020-01-01T00:00:00.000Z"},
            {"key": "fanmade001", "name": "Fan trailer", "site": "YouTube", "type": "Trailer", "official": false, "published_at": "2022-01-01T00:00:00.000Z"},
            {"key": "official01", "name": "Official trailer", "site": "YouTube", "type": "Trailer", "official": true, "published_at": "1999-03-01T00:00:00.000Z"},
            {"key": "vimeo00001", "name": "Elsewhere", "site": "Vimeo", "type": "Trailer", "official": true, "published_at": "2023-01-01T00:00:00.000Z"}
          ]},
          "images": {
            "backdrops": [
              {"file_path": "/low.jpg", "width": 1920, "height": 1080, "vote_average": 1.0, "vote_count": 1},
              {"file_path": "/high.jpg", "width": 1920, "height": 1080, "vote_average": 5.5, "vote_count": 9},
              {"file_path": "", "width": 1, "height": 1}
            ],
            "posters": []
          },
          "watch/providers": {"results": {
            "RO": {"link": "https://example.test/ro",
                   "rent": [${provider(2, "Apple TV", 1)}, ${provider(3, "Rakuten", 0)}],
                   "buy": [${provider(2, "Apple TV", 1)}],
                   "flatrate": [${provider(8, "Netflix", 4)}]},
            "US": {"flatrate": [${provider(9, "Prime")}]}
          }},
          "recommendations": {"results": [
            {"id": 604, "title": "The Matrix Reloaded", "release_date": "2003-05-15", "vote_average": 7.0, "poster_path": "/r.jpg"},
            {"id": 605, "title": "Undated", "release_date": "", "vote_average": 0, "poster_path": null}
          ]}
        }
    """.trimIndent()

    @Test
    fun projectsTheBody() {
        val d = parseMovieDetails(body)
        assertEquals("Free your mind.", d.tagline)
        assertEquals("Lana Wachowski", d.director)
        assertEquals(26000L, d.votes)
        assertNull("an empty homepage is no homepage", d.homepage)
        assertEquals("tt0133093", d.imdbId)
        assertEquals(63_000_000L, d.budget)
        assertNull("TMDB's 0 means unknown", d.revenue)
        assertEquals(listOf("Keanu Reeves", "Carrie-Anne Moss"), d.cast.map { it.name })
        assertEquals("/k.jpg", d.cast.first().profilePath)
    }

    @Test
    fun ranksVideosAndPicksTheOfficialTrailer() {
        val d = parseMovieDetails(body)
        assertEquals(listOf("official01", "fanmade001", "teaser0001"), d.videos.map { it.key })
        assertEquals("https://www.youtube.com/watch?v=official01", d.trailerUrl)
        assertEquals("1999-03-01", d.videos.first().publishedDay)
    }

    @Test
    fun ordersImagesByVoteAndDropsPathlessOnes() {
        val d = parseMovieDetails(body)
        assertEquals(listOf("/high.jpg", "/low.jpg"), d.backdrops.map { it.path })
        assertTrue(d.posters.isEmpty())
    }

    @Test
    fun streamingListsOneTilePerProviderInOfferOrder() {
        val s = parseMovieDetails(body).streaming
        assertNotNull(s)
        assertEquals("https://example.test/ro", s!!.link)
        assertEquals(listOf("Netflix", "Rakuten", "Apple TV"), s.providers.map { it.name })
        assertEquals(StreamingOffer.RENT, s.providers.last().offer)
    }

    @Test
    fun streamingTellsNoSectionFromNobodyListed() {
        assertNull(parseMovieDetails("""{"id":1}""").streaming)
        val empty = parseMovieDetails("""{"id":1,"watch/providers":{"results":{}}}""").streaming
        assertNotNull(empty)
        assertTrue(empty!!.providers.isEmpty())
        // Pruned to another country: no answer is held for this one.
        assertNull(parseMovieDetails("""{"id":1,"watch/providers":{"results":{},"sofalog_region":"DE"}}""").streaming)
    }

    @Test
    fun similarKeepsTmdbOrderAndBlanksUnknowns() {
        val similar = parseMovieDetails(body).similar
        assertEquals(listOf(604L, 605L), similar.map { it.tmdbId })
        assertEquals("2003", similar[0].year)
        assertNull(similar[1].year)
        assertNull(similar[1].rating)
    }

    @Test
    fun pruningKeepsOneCountryAndMarksIt() {
        val pruned = pruneWatchProviders(body)
        assertFalse(pruned.contains("Prime"))
        assertTrue(pruned.contains("Netflix"))
        assertEquals(parseMovieDetails(body).streaming, parseMovieDetails(pruned).streaming)
        assertEquals("pruning twice changes nothing", pruned, pruneWatchProviders(pruned))
        assertEquals("a body without the section is left alone", """{"id":1}""", pruneWatchProviders("""{"id":1}"""))
    }

    @Test
    fun aBodyIsIncompleteUntilEverySectionIsThereAndPrunedHere() {
        assertTrue("the pre-append shape", movieSnapshotIncomplete("""{"id":1,"title":"x"}"""))
        assertTrue("every country still in it", movieSnapshotIncomplete(body))
        assertFalse(movieSnapshotIncomplete(pruneWatchProviders(body)))
        assertTrue("pruned to another country", movieSnapshotIncomplete(pruneWatchProviders(body), region = "DE"))
        assertTrue(movieSnapshotIncomplete("not json"))
    }

    @Test
    fun unreadableBodyReadsAsNoExtras() {
        assertEquals(MovieDetails.EMPTY, parseMovieDetails("not json"))
    }
}
