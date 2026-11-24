package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ShowSnapshotTest {
    private val body = """
        {
          "name": "The Show", "tagline": " A tagline ", "homepage": "", "original_language": "en",
          "popularity": 81.25, "vote_count": 1200, "last_air_date": "2024-05-01", "in_production": true,
          "number_of_seasons": 2, "number_of_episodes": 18, "episode_run_time": [45, 60],
          "created_by": [{"name": "Ann Writer"}, {"name": ""}, {"name": "Bo Maker"}],
          "networks": [{"name": "HBO"}],
          "next_episode_to_air": {"season_number": 3, "episode_number": 1, "air_date": "2026-12-01"},
          "seasons": [
            {"season_number": 0, "vote_average": 0},
            {"season_number": 1, "vote_average": 8.1},
            {"season_number": 2, "vote_average": 7.4}
          ],
          "aggregate_credits": {"cast": [
            {"name": "Second Billed", "order": 1, "total_episode_count": 18,
             "roles": [{"character": "Sam", "episode_count": 18}]},
            {"name": "", "order": 0, "total_episode_count": 3, "roles": []},
            {"name": "Lead Actor", "order": 0, "total_episode_count": 12, "profile_path": "/lead.jpg",
             "roles": [{"character": "Guest", "episode_count": 2}, {"character": "Hero", "episode_count": 10}, {"character": ""}]},
            {"name": "Same Billing", "order": 1, "total_episode_count": 20, "roles": []}
          ]},
          "external_ids": {"imdb_id": "tt0000001"},
          "videos": {"results": [
            {"key": "teaser0001", "name": "Teaser", "site": "YouTube", "type": "Teaser", "official": true, "published_at": "2024-01-01T00:00:00.000Z"},
            {"key": "trailer001", "name": "Trailer", "site": "YouTube", "type": "Trailer", "official": true, "published_at": "2023-01-01T00:00:00.000Z"}
          ]},
          "images": {"backdrops": [{"file_path": "/b.jpg", "width": 1280, "height": 720}], "posters": []},
          "watch/providers": {"results": {"RO": {"link": "https://example.test/ro", "flatrate": [{"provider_id": 8, "provider_name": "Netflix"}]}}, "sofalog_region": "RO"},
          "recommendations": {"results": [
            {"id": 7, "name": "Another Show", "first_air_date": "2019-03-04", "vote_average": 7.9, "poster_path": "/p.jpg"},
            {"id": 8, "name": "", "first_air_date": "2020-01-01"}
          ]}
        }
    """.trimIndent()

    @Test
    fun projectsTheBody() {
        val d = parseShowDetails(body)
        assertEquals("A tagline", d.tagline)
        assertNull(d.homepage)
        assertEquals(listOf("Ann Writer", "Bo Maker"), d.createdBy)
        assertEquals(listOf("HBO"), d.networks)
        assertEquals(1200L, d.votes)
        assertEquals("tt0000001", d.imdbId)
        assertEquals("2024-05-01", d.lastAirDate)
        assertTrue(d.inProduction)
        assertEquals(2, d.seasonsCount)
        assertEquals(18, d.episodesCount)
        assertEquals(45, d.episodeRuntimeMin)
        assertEquals(NextEpisode(3, 1, "2026-12-01"), d.nextEpisode)
        assertEquals("https://www.youtube.com/watch?v=trailer001", d.trailerUrl)
        assertEquals(1, d.backdrops.size)
    }

    @Test
    fun seasonScoresLeaveOutSeasonsWithoutVotes() {
        assertEquals(mapOf(1 to 8.1, 2 to 7.4), parseShowDetails(body).seasonScores)
    }

    @Test
    fun aggregateCastIsInBillingOrderWithRolesJoinedMostEpisodesFirst() {
        val cast = parseShowDetails(body).cast
        assertEquals(listOf("Lead Actor", "Same Billing", "Second Billed"), cast.map { it.name })
        assertEquals(CastMember("Lead Actor", "Hero / Guest", "/lead.jpg", 12), cast.first())
        assertNull(cast[1].character)
        assertEquals(20, cast[1].episodes)
    }

    @Test
    fun similarReadsTheShowFieldsAndDropsUntitledEntries() {
        assertEquals(
            listOf(SimilarTitle(7, "Another Show", "2019", 7.9, "/p.jpg")),
            parseShowDetails(body).similar,
        )
    }

    @Test
    fun streamingTellsNoSectionFromNobodyListed() {
        assertEquals(listOf("Netflix"), parseShowDetails(body).streaming?.providers?.map { it.name })
        assertNull(parseShowDetails("""{"name": "Bare"}""").streaming)
        val nobody = """{"watch/providers": {"results": {}, "sofalog_region": "RO"}}"""
        assertEquals(Streaming("RO", null, emptyList()), parseShowDetails(nobody).streaming)
    }

    @Test
    fun aBodyIsIncompleteUntilEverySectionIsThereAndPrunedHere() {
        assertFalse(showSnapshotIncomplete(body))
        // What a show tracked before the page read these sections holds: the bare root document.
        assertTrue(showSnapshotIncomplete("""{"name": "Bare", "seasons": []}"""))
        assertTrue(showSnapshotIncomplete(body, region = "DE"))
        assertTrue(showSnapshotIncomplete("not json"))
    }

    @Test
    fun unreadableBodyReadsAsNoExtras() {
        assertEquals(ShowDetails.EMPTY, parseShowDetails("not json"))
    }
}
