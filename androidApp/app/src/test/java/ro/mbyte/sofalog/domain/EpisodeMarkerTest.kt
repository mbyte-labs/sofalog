package ro.mbyte.sofalog.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class EpisodeMarkerTest {
    @Test
    fun finaleTypesWin() {
        assertEquals(EpisodeMarker.SEASON_FINALE, episodeMarker("finale", 10))
        assertEquals(EpisodeMarker.MID_SEASON_FINALE, episodeMarker("mid_season", 8))
        // A one-episode season's only episode is its finale, as on the server.
        assertEquals(EpisodeMarker.SEASON_FINALE, episodeMarker("finale", 1))
    }

    @Test
    fun episodeOneIsThePremiere() {
        assertEquals(EpisodeMarker.SEASON_PREMIERE, episodeMarker("standard", 1))
        assertEquals(EpisodeMarker.SEASON_PREMIERE, episodeMarker(null, 1))
    }

    @Test
    fun plainEpisodesHaveNone() {
        assertNull(episodeMarker("standard", 4))
        assertNull(episodeMarker(null, 4))
    }
}
