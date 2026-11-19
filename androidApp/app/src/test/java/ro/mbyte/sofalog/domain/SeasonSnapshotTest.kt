package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SeasonSnapshotTest {
    private val body = """
        {
          "season_number": 2, "name": "Season 2", "vote_average": 7.8,
          "episodes": [
            {"episode_number": 1, "name": "One", "vote_average": 8.2, "vote_count": 41, "crew": [{"name": "Someone"}],
             "guest_stars": [
               {"id": 9, "name": "Later Guest", "character": "", "order": 7, "credit_id": "abc", "popularity": 3.1},
               {"name": "", "order": 0},
               {"name": "A Guest", "character": "Visitor", "profile_path": "/guest.jpg", "order": 2}
             ]},
            {"episode_number": 2, "name": "Two", "vote_average": 0},
            {"episode_number": 3, "name": "Three", "vote_average": 6.5}
          ],
          "aggregate_credits": {"cast": [
            {"name": "Second Billed", "order": 1, "total_episode_count": 3,
             "roles": [{"character": "Sam", "episode_count": 3}]},
            {"name": "", "order": 0, "total_episode_count": 1, "roles": []},
            {"name": "Lead Actor", "order": 0, "total_episode_count": 2, "profile_path": "/lead.jpg",
             "roles": [{"character": "Guest", "episode_count": 1}, {"character": "Hero", "episode_count": 2}]}
          ]},
          "credits": {
            "cast": [
              {"name": "Regular Two", "character": "Sam", "order": 1},
              {"name": "Regular One", "character": "Hero", "profile_path": "/one.jpg", "order": 0}
            ],
            "crew": [{"name": "A Director", "job": "Director"}]
          }
        }
    """.trimIndent()

    @Test
    fun projectsTheBody() {
        val d = parseSeasonDetails(body)
        assertEquals(7.8, d.score!!, 0.0)
        assertEquals(mapOf(1 to 8.2, 3 to 6.5), d.episodeScores)
        assertEquals(listOf("Lead Actor", "Second Billed"), d.cast.map { it.name })
        assertEquals("Hero / Guest", d.cast.first().character)
        assertEquals(2, d.cast.first().episodes)
        assertEquals("/lead.jpg", d.cast.first().profilePath)
    }

    @Test
    fun projectsWhatTheEpisodePageReads() {
        val d = parseSeasonDetails(body)
        assertEquals(mapOf(1 to 41L), d.episodeVotes)
        assertEquals(
            listOf(CastMember("Regular One", "Hero", "/one.jpg"), CastMember("Regular Two", "Sam", null)),
            d.regulars,
        )
        // Billing order, the nameless entry dropped, an empty character read as none.
        assertEquals(
            mapOf(1 to listOf(CastMember("A Guest", "Visitor", "/guest.jpg"), CastMember("Later Guest", null, null))),
            d.guestStars,
        )
    }

    @Test
    fun aSeasonWithoutVotesHasNoScore() {
        assertNull(parseSeasonDetails("""{"vote_average": 0, "episodes": []}""").score)
    }

    @Test
    fun compactingKeepsWhatThePageReadsAndDropsTheRest() {
        val compact = compactSeasonBody(body)
        assertEquals(parseSeasonDetails(body), parseSeasonDetails(compact))
        // A guest is kept as the four fields a cast card shows; the crew goes, at both levels.
        assertTrue(compact.contains("guest_stars"))
        assertFalse(compact.contains("credit_id"))
        assertFalse(compact.contains("popularity"))
        assertFalse(compact.contains("crew"))
        assertFalse(compact.contains("A Director"))
    }

    @Test
    fun aBodyStoredBeforeTheRegularsWereKeptIsIncomplete() {
        assertTrue(seasonSnapshotIncomplete("""{"vote_average": 7.8, "episodes": [], "aggregate_credits": {"cast": []}}"""))
        assertTrue(seasonSnapshotIncomplete("not json"))
        assertFalse(seasonSnapshotIncomplete(compactSeasonBody(body)))
        // A season TMDB credits nobody on is still complete once it has been compacted.
        assertFalse(seasonSnapshotIncomplete(compactSeasonBody("""{"vote_average": 0, "episodes": []}""")))
    }

    @Test
    fun aMissingOrUnreadableBodyReadsAsNoExtras() {
        assertEquals(SeasonDetails.EMPTY, parseSeasonDetails(null))
        assertEquals(SeasonDetails.EMPTY, parseSeasonDetails("not json"))
        assertEquals("not json", compactSeasonBody("not json"))
    }

    @Test
    fun theHistoryFeedReadsTheSameEpisodeScores() {
        assertEquals(parseSeasonDetails(body).episodeScores, parseEpisodeScores(body))
        assertEquals(parseSeasonDetails(body).episodeScores, parseEpisodeScores(compactSeasonBody(body)))
        // An episode without votes is absent, not zero.
        assertEquals(
            mapOf(1 to 8.2),
            parseEpisodeScores("""{"episodes": [{"episode_number": 1, "vote_average": 8.2}, {"episode_number": 2, "vote_average": 0}]}"""),
        )
        assertTrue(parseEpisodeScores(null).isEmpty())
        assertTrue(parseEpisodeScores("not json").isEmpty())
    }
}
