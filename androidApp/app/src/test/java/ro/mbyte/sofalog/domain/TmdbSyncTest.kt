package ro.mbyte.sofalog.domain

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.time.Duration
import java.time.Instant

class TmdbSyncTest {
    @Serializable
    private data class Rounding(val value: Double, val want: Int)

    @Serializable
    private data class Backoff(val attempts: Int, val seconds: Long)

    @Serializable
    private data class SyncFile(@SerialName("rating_rounding") val rounding: List<Rounding>, val backoff: List<Backoff>)

    /** The shared cases the server's `tmdbaccount` tests run too (SPEC D-040). */
    @Test
    fun sharedFixtures() {
        val file = File("../../fixtures/video-rules/tmdb-sync.json")
        assertTrue("fixture not found at ${file.absolutePath}", file.exists())
        val cases = Json.decodeFromString<SyncFile>(file.readText())
        assertTrue(cases.rounding.isNotEmpty() && cases.backoff.isNotEmpty())
        for (c in cases.rounding) assertEquals("round ${c.value}", c.want, roundTmdbRating(c.value))
        for (c in cases.backoff) assertEquals("backoff ${c.attempts}", Duration.ofSeconds(c.seconds), syncBackoff(c.attempts))
    }

    @Test
    fun theBreakerOpensAfterThreeOutagesInARowAndASuccessClosesIt() {
        val breaker = ProviderBreaker()
        breaker.observe(true)
        breaker.observe(true)
        assertFalse(breaker.open)
        breaker.observe(false)
        breaker.observe(true)
        breaker.observe(true)
        assertFalse(breaker.open)
        breaker.observe(true)
        assertTrue(breaker.open)
        breaker.observe(false)
        assertFalse(breaker.open)
    }

    @Test
    fun a401IsTheSessionsFaultOnlyWhenTheCallCarriedOne() {
        assertEquals(TmdbFailure.SESSION_INVALID, tmdbFailure(401, userSession = true, statusCode = 3))
        assertEquals(TmdbFailure.SESSION_INVALID, tmdbFailure(401, userSession = true, statusCode = null))
        // TMDB's status 7 is "invalid API key": the app's fault, not the link's.
        assertEquals(TmdbFailure.BAD_KEY, tmdbFailure(401, userSession = true, statusCode = 7))
        assertEquals(TmdbFailure.BAD_KEY, tmdbFailure(401, userSession = false, statusCode = 3))
        assertEquals(TmdbFailure.NOT_FOUND, tmdbFailure(404, true, null))
        assertEquals(TmdbFailure.REFUSED, tmdbFailure(400, true, null))
        assertEquals(TmdbFailure.REFUSED, tmdbFailure(422, true, null))
        assertEquals(TmdbFailure.UNAVAILABLE, tmdbFailure(429, true, null))
        assertEquals(TmdbFailure.UNAVAILABLE, tmdbFailure(503, false, null))
    }

    @Test
    fun whatTheWorkerDoesWithEachOutcome() {
        assertEquals(SendOutcome.SENT, classifySend(null))
        assertEquals(SendOutcome.BROKEN, classifySend(TmdbFailure.SESSION_INVALID))
        assertEquals(SendOutcome.DROP, classifySend(TmdbFailure.NOT_FOUND))
        assertEquals(SendOutcome.DROP, classifySend(TmdbFailure.REFUSED))
        assertEquals(SendOutcome.DEFER, classifySend(TmdbFailure.UNAVAILABLE))
        assertEquals(SendOutcome.DEFER, classifySend(TmdbFailure.BAD_KEY))
    }

    @Test
    fun ratingPathsAndMediaTypes() {
        assertEquals("movie/603/rating", ratingPath("movie", 603, 0, 0))
        assertEquals("tv/1399/rating", ratingPath("show", 1399, 0, 0))
        assertEquals("tv/1399/season/2/episode/9/rating", ratingPath("episode", 1399, 2, 9))
        assertEquals("movie", accountMediaType("movie"))
        assertEquals("tv", accountMediaType("show"))
    }

    @Test
    fun theApprovalPageCarriesTheTokenAndTheWayBack() {
        assertEquals(
            "https://www.themoviedb.org/authenticate/a%2Fb?redirect_to=sofalog%3A%2F%2Ftmdb-link",
            authenticateUrl("a/b"),
        )
    }

    @Test
    fun readingTheReturnFromTmdbsPage() {
        assertEquals(LinkReturn("tok", false), parseLinkReturn("sofalog://tmdb-link?request_token=tok&approved=true"))
        assertEquals(LinkReturn("tok", true), parseLinkReturn("sofalog://tmdb-link?request_token=tok&denied=true"))
        assertEquals(LinkReturn(null, false), parseLinkReturn("sofalog://tmdb-link"))
        assertNull(parseLinkReturn("https://example.com/?request_token=tok"))
        assertNull(parseLinkReturn(null))
    }

    @Test
    fun aReturnIsActedOnOnlyForThePendingToken() {
        val started = "2026-11-19T10:00:00Z"
        val soon = Instant.parse("2026-11-19T10:10:00Z")
        // Nothing pending: anything that fires the app's link is ignored.
        assertEquals(LinkDecision.IGNORE, linkReturnDecision(null, null, soon, LinkReturn("tok", false)))
        // Another token than the pending one — not ours.
        assertEquals(LinkDecision.IGNORE, linkReturnDecision("tok", started, soon, LinkReturn("other", false)))
        assertEquals(LinkDecision.DENIED, linkReturnDecision("tok", started, soon, LinkReturn("tok", true)))
        assertEquals(LinkDecision.EXCHANGE, linkReturnDecision("tok", started, soon, LinkReturn("tok", false)))
        // Coming back by hand, with no return at all, still tries the exchange.
        assertEquals(LinkDecision.EXCHANGE, linkReturnDecision("tok", started, soon, null))
        assertEquals(LinkDecision.EXPIRED, linkReturnDecision("tok", started, Instant.parse("2026-11-19T10:45:00Z"), null))
        assertEquals(LinkDecision.EXPIRED, linkReturnDecision("tok", "garbage", soon, null))
    }

    @Test
    fun pickingAnActionPresetsTheSwitch() {
        assertEquals(KindSetup(SyncAction.PULL, true), KindSetup().pick(SyncAction.PULL))
        assertEquals(KindSetup(SyncAction.PUSH, true), KindSetup().pick(SyncAction.PUSH))
        assertEquals(KindSetup(SyncAction.SKIP, false), KindSetup(SyncAction.PUSH, true).pick(SyncAction.SKIP))
        val draft = SetupDraft(ratings = KindSetup(SyncAction.PULL, true), favorites = KindSetup(SyncAction.PUSH, false))
        assertTrue(draft.pulls)
        assertTrue(draft.pushes)
        assertFalse(SetupDraft().pulls || SetupDraft().pushes)
    }

    @Test
    fun secretsAreBlankedWhereverTheyRide() {
        val text = "GET https://api.themoviedb.org/3/account?api_key=abc123&session_id=s3cr3t&page=1 " +
            "{\"request_token\": \"tok\"} Authorization: Bearer eyJhbGciOi.xyz"
        val redacted = redactSecrets(text)
        for (secret in listOf("abc123", "s3cr3t", "\"tok\"", "eyJhbGciOi")) assertFalse(secret, secret in redacted)
        assertTrue("page=1" in redacted)
    }

    @Test
    fun theLinesThePageShows() {
        assertEquals("Everything has been sent.", pendingLine(0))
        assertEquals("1 change is waiting to be sent.", pendingLine(1))
        assertEquals("4 changes are waiting to be sent.", pendingLine(4))
        assertEquals("Preparing…", pullProgressLine(TmdbPullJob()))
        assertEquals(
            "Importing ratings — movies, page 2 of 3. 21 entries so far.",
            pullProgressLine(TmdbPullJob(section = "ratings", list = "movies", page = 2, totalPages = 3, entries = 21)),
        )
    }
}
