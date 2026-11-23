package ro.mbyte.sofalog.ui.common

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import kotlinx.coroutines.CancellationException
import ro.mbyte.sofalog.SofalogApp
import ro.mbyte.sofalog.data.RestoreRepository
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.tmdbaccount.TmdbAccountService
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.data.tmdb.TmdbException
import ro.mbyte.sofalog.domain.UserZone
import ro.mbyte.sofalog.domain.isDateOnlyWatch
import ro.mbyte.sofalog.ui.theme.OnBadge
import java.time.Instant
import java.time.LocalDate
import java.time.YearMonth
import java.time.format.DateTimeFormatter
import java.util.Locale

@Composable
fun repository(): VideoRepository = (LocalContext.current.applicationContext as SofalogApp).repository

@Composable
fun tmdbAccount(): TmdbAccountService = (LocalContext.current.applicationContext as SofalogApp).tmdbAccount

@Composable
fun restoreRepository(): RestoreRepository = (LocalContext.current.applicationContext as SofalogApp).restore

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
    TmdbException.Reason.SESSION_INVALID -> "TMDB no longer accepts this link."
    TmdbException.Reason.REFUSED -> "TMDB refused the request."
    null -> "Something went wrong."
}

private val dayFormat = DateTimeFormatter.ofPattern("dd.MM.yyyy")
private val instantFormat = DateTimeFormatter.ofPattern("dd.MM.yyyy HH:mm")

/** A plain YYYY-MM-DD date. No zone is involved, so none can move it. */
fun formatDay(date: String?): String? =
    date?.let { runCatching { LocalDate.parse(it).format(dayFormat) }.getOrNull() }

/** A stored UTC instant, shown in the user's zone. */
fun formatInstant(instant: String): String = runCatching {
    Instant.parse(instant).atZone(UserZone.current).format(instantFormat)
}.getOrDefault(instant)

private val historyInstantFormat = DateTimeFormatter.ofPattern("EEE, d MMM, HH:mm", Locale.ENGLISH)
private val monthFormat = DateTimeFormatter.ofPattern("MMMM yyyy", Locale.ENGLISH)

/** The history card's line: weekday, day and time in the user's zone ("Sat, 15 Aug, 21:05"). */
fun formatHistoryInstant(instant: String): String = runCatching {
    Instant.parse(instant).atZone(UserZone.current).format(historyInstantFormat)
}.getOrDefault(instant)

private val watchDayFormat = DateTimeFormatter.ofPattern("EEE, d MMM yyyy", Locale.ENGLISH)
private val watchTimeFormat = DateTimeFormatter.ofPattern("HH:mm", Locale.ENGLISH)
private val shortDayFormat = DateTimeFormatter.ofPattern("d MMM yyyy", Locale.ENGLISH)

/**
 * One watch on an item's own timeline, in the user's zone: "Sat, 14 Nov 2026 · 21:05". A watch
 * logged "on its release date" sits at midnight and shows its day alone — it never had a time.
 */
fun formatWatchInstant(instant: String): String = runCatching {
    val zone = UserZone.current
    val at = Instant.parse(instant).atZone(zone)
    val day = at.format(watchDayFormat)
    if (isDateOnlyWatch(instant, zone)) day else "$day · ${at.format(watchTimeFormat)}"
}.getOrDefault(instant)

/** A watch's day without the weekday: "16 Jul 2010". */
fun formatWatchDay(instant: String): String = runCatching {
    Instant.parse(instant).atZone(UserZone.current).format(shortDayFormat)
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
            .clip(PosterShape)
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

/** The poster's corner rounding: what a card clips its badges to. */
val PosterShape = RoundedCornerShape(8.dp)

/** A corner badge on a poster; [shape] rounds only the corner that faces the artwork. */
@Composable
fun PosterBadge(text: String, background: Color, content: Color, shape: Shape, modifier: Modifier) {
    Text(
        text,
        style = MaterialTheme.typography.labelSmall,
        color = content,
        fontWeight = FontWeight.SemiBold,
        modifier = modifier.clip(shape).background(background).padding(horizontal = 8.dp, vertical = 4.dp),
    )
}

/** A tab in a poster's top-left corner stack; only the stack's [last] tab rounds its bottom-right corner. */
@Composable
fun PosterTab(text: String, background: Color, last: Boolean) {
    Text(
        text,
        style = MaterialTheme.typography.labelSmall,
        color = OnBadge,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier
            .clip(if (last) RoundedCornerShape(bottomEnd = 6.dp) else RectangleShape)
            .background(background)
            .padding(horizontal = 9.dp, vertical = 4.dp),
    )
}

/**
 * A library state on a poster or a still: a white glyph on the state's own colour, the corner
 * badge's height. The caller lays tabs out in a row and clips the row.
 */
@Composable
fun MarkTab(icon: ImageVector, label: String, color: Color) {
    Box(Modifier.background(color).padding(horizontal = 8.dp, vertical = 4.dp)) {
        Icon(icon, contentDescription = label, tint = OnBadge, modifier = Modifier.size(16.dp))
    }
}

/** One pick out of a few, as a full-width segmented row: the filter rows of the grids. */
@Composable
fun <T> SegmentedChoice(options: List<Pair<T, String>>, selected: T, onSelect: (T) -> Unit) {
    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
        options.forEachIndexed { index, (value, label) ->
            SegmentedButton(
                selected = selected == value,
                onClick = { onSelect(value) },
                shape = SegmentedButtonDefaults.itemShape(index, options.size),
                icon = {},
            ) { Text(label, maxLines = 1, overflow = TextOverflow.Ellipsis) }
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
