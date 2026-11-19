package ro.mbyte.sofalog.ui.details.parts

import org.junit.Assert.assertEquals
import org.junit.Test

class FormatRuntimeTest {
    @Test
    fun readsMinutesAsALength() {
        assertEquals("2h 12m", formatRuntime(132))
        assertEquals("45m", formatRuntime(45))
        assertEquals("2h", formatRuntime(120))
        assertEquals("1h 1m", formatRuntime(61))
    }
}
