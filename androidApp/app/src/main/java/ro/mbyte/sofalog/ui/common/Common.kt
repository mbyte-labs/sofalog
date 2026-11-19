package ro.mbyte.sofalog.ui.common

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import kotlinx.coroutines.CancellationException
import ro.mbyte.sofalog.SofalogApp
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.data.tmdb.TmdbException
import ro.mbyte.sofalog.domain.isDateOnlyWatch
import java.time.Instant
import java.time.LocalDate
import java.time.YearMonth
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

@Composable
fun repository(): VideoRepository = (LocalContext.current.applicationContext as SofalogApp).repository

/** runCatching that lets coroutine cancellation through. */
suspend fun <T> attempt(block: suspend () -> T): Result<T> = try {
    Result.success(block())
} catch (e: CancellationException) {
    throw e
} catch (e: Exception) {
    Result.failure(e)
}

fun Throwable.userMessage(): String = when ((this as? TmdbException)?.reason) {
    TmdbException.Reason.NO_KEY ->
        "This build has no TMDB key. Add tmdb.apiKey to androidApp/local.properties and rebuild."
    TmdbException.Reason.BAD_KEY -> "TMDB refused the app's key."
    TmdbException.Reason.NOT_FOUND -> "TMDB doesn't list this title."
    TmdbException.Reason.UNAVAILABLE -> "Couldn't reach TMDB. Check your connection and try again."
    null -> "Something went wrong."
}

private val dayFormat = DateTimeFormatter.ofPattern("dd.MM.yyyy")
private val instantFormat = DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm")

/** A plain YYYY-MM-DD date. No zone is involved, so none can move it. */
fun formatDay(date: String?): String? =
    date?.let { runCatching { LocalDate.parse(it).format(dayFormat) }.getOrNull() }

/** A stored UTC instant, shown in the device's zone. */
fun formatInstant(instant: String): String = runCatching {
    Instant.parse(instant).atZone(ZoneId.systemDefault()).format(instantFormat)
}.getOrDefault(instant)

private val historyInstantFormat = DateTimeFormatter.ofPattern("EEE, d MMM, HH:mm", Locale.ENGLISH)
private val monthFormat = DateTimeFormatter.ofPattern("MMMM yyyy", Locale.ENGLISH)

/** The history card's line: weekday, day and time in the device's zone ("Sat, 15 Aug, 21:05"). */
fun formatHistoryInstant(instant: String): String = runCatching {
    Instant.parse(instant).atZone(ZoneId.systemDefault()).format(historyInstantFormat)
}.getOrDefault(instant)

private val watchDayFormat = DateTimeFormatter.ofPattern("EEE, d MMM yyyy", Locale.ENGLISH)
private val watchTimeFormat = DateTimeFormatter.ofPattern("HH:mm", Locale.ENGLISH)
private val shortDayFormat = DateTimeFormatter.ofPattern("d MMM yyyy", Locale.ENGLISH)

/**
 * One watch on an item's own timeline, in the device's zone: "Sat, 14 Nov 2026 · 21:05". A watch
 * logged "on its release date" sits at midnight and shows its day alone — it never had a time.
 */
fun formatWatchInstant(instant: String): String = runCatching {
    val zone = ZoneId.systemDefault()
    val at = Instant.parse(instant).atZone(zone)
    val day = at.format(watchDayFormat)
    if (isDateOnlyWatch(instant, zone)) day else "$day · ${at.format(watchTimeFormat)}"
}.getOrDefault(instant)

/** A watch's day without the weekday: "16 Jul 2010". */
fun formatWatchDay(instant: String): String = runCatching {
    Instant.parse(instant).atZone(ZoneId.systemDefault()).format(shortDayFormat)
}.getOrDefault(instant)

/** A month heading: "August 2026". */
fun formatMonth(month: YearMonth): String = month.format(monthFormat)

fun episodeCode(season: Int, number: Int): String = "S${season}xE$number"

fun formatRuntime(minutes: Int?): String? = minutes?.let {
    if (it >= 60) "${it / 60}h ${it % 60}m" else "${it}m"
}

fun formatScore(score: Double?): String? = score?.let { "★ %.1f".format(it) }

@Composable
fun Poster(path: String?, title: String, modifier: Modifier = Modifier) {
    Box(
        modifier
            .aspectRatio(2f / 3f)
            .clip(RoundedCornerShape(8.dp))
            .background(MaterialTheme.colorScheme.surfaceVariant),
        contentAlignment = Alignment.Center,
    ) {
        val url = TmdbClient.posterUrl(path)
        if (url == null) {
            Text(
                title,
                Modifier.padding(8.dp),
                style = MaterialTheme.typography.labelMedium,
                textAlign = TextAlign.Center,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        } else {
            AsyncImage(
                model = url,
                contentDescription = title,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        }
    }
}

@Composable
fun CenteredLoading(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
}

@Composable
fun CenteredMessage(text: String, modifier: Modifier = Modifier, action: (@Composable () -> Unit)? = null) {
    Box(modifier.fillMaxSize().padding(24.dp), contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(text, textAlign = TextAlign.Center, color = MaterialTheme.colorScheme.onSurfaceVariant)
            action?.invoke()
        }
    }
}

@Composable
fun ConfirmDialog(title: String, text: String, confirm: String, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { Text(text) },
        confirmButton = { TextButton(onClick = onConfirm) { Text(confirm) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
