package ro.mbyte.sofalog.ui.details.parts

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.domain.EpisodeMarker
import ro.mbyte.sofalog.ui.common.MarkTab
import ro.mbyte.sofalog.ui.common.formatDay
import ro.mbyte.sofalog.ui.theme.FinaleBadge
import ro.mbyte.sofalog.ui.theme.SeasonPremiereBadge
import ro.mbyte.sofalog.ui.theme.WatchedItemColor
import java.util.Locale

/*
 * The pieces a show's parts — its seasons, and its episodes after them — add to the shared
 * sections: the average of the user's episode ratings, the "you dropped this show" banner, and
 * the episode card of a season's grid (SeasonDetailsPage.svelte).
 */

/**
 * The mean of the user's episode ratings — a read, not something to set. On the show page it sits
 * under the show's own rating bar; on a season page it stands where the bar would, because a
 * season is not itself rated.
 */
@Composable
fun EpisodeAverage(label: String, mean: Double?, note: String, emptyNote: String) {
    Column(Modifier.widthIn(max = 480.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        SectionLabel(label)
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Icon(
                if (mean == null) DetailIcons.HeartOutline else DetailIcons.Heart,
                contentDescription = null,
                modifier = Modifier.size(24.dp),
                tint = if (mean == null) MaterialTheme.colorScheme.onSurfaceVariant else HeartRed,
            )
            Row(verticalAlignment = Alignment.Bottom) {
                Text(mean?.let { "%.1f".format(Locale.ENGLISH, it) } ?: "–", fontSize = 26.sp, fontWeight = FontWeight.Bold)
                Text(
                    "/10",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(start = 2.dp, bottom = 4.dp),
                )
            }
        }
        Note(if (mean == null) emptyNote else note)
    }
}

/**
 * Said on a page below a dropped show: the show's state changes how its parts read, and resuming
 * is the show page's to do — the whole banner leads there.
 */
@Composable
fun DroppedNotice(showTitle: String, page: Dp, onOpenShow: () -> Unit) {
    val scheme = MaterialTheme.colorScheme
    Surface(
        onClick = onOpenShow,
        shape = RoundedCornerShape(12.dp),
        color = scheme.error.copy(alpha = 0.12f),
        border = BorderStroke(1.dp, scheme.error.copy(alpha = 0.5f)),
        modifier = Modifier.fillMaxWidth().padding(start = page, end = page, top = 20.dp),
    ) {
        Text(
            buildAnnotatedString {
                append("You've dropped ")
                withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append(showTitle) }
                append(". Marking episodes still works — ")
                withStyle(SpanStyle(color = scheme.primary, fontWeight = FontWeight.SemiBold)) { append("resume it on the show page") }
                append(".")
            },
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.heightIn(min = 48.dp).padding(horizontal = 16.dp, vertical = 12.dp),
        )
    }
}

private val BadgeShape = RoundedCornerShape(6.dp)

/**
 * One episode of a season's grid: its still wearing what is known about it — a premiere or finale
 * tab in the top-left corner with the search card's watched tab under it, the user's rating
 * top-right and TMDB's score along the foot — then its number and title, and
 * when it aired and for how long. A tap opens it; a long-press is the page's to use.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun EpisodeCard(
    showTitle: String,
    number: Int,
    title: String?,
    airDate: String?,
    runtime: Int?,
    stillPath: String?,
    watched: Boolean,
    userRating: Int?,
    score: Double?,
    marker: EpisodeMarker?,
    modifier: Modifier,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
) {
    val scheme = MaterialTheme.colorScheme
    val haptics = LocalHapticFeedback.current
    val code = "E%02d".format(Locale.ENGLISH, number)
    val name = title ?: "Episode $number"
    Column(
        modifier.clip(TileShape).combinedClickable(
            role = Role.Button,
            onLongClickLabel = "More about this episode",
            onLongClick = {
                haptics.performHapticFeedback(HapticFeedbackType.LongPress)
                onLongClick()
            },
            onClick = onClick,
        ),
    ) {
        val hue = remember(showTitle, number) { (hueFor(showTitle) + number * 28) % 360 }
        Box(
            Modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(TileShape).background(hueBrush(hue, 0.42f, 0.32f, 0.14f)),
            contentAlignment = Alignment.Center,
        ) {
            val still = TmdbClient.stillUrl(stillPath)
            if (still == null) {
                Text(code, fontSize = 22.sp, fontWeight = FontWeight.Bold, color = Color.White.copy(alpha = 0.5f))
            } else {
                AsyncImage(still, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            }
            // Tabs stacked flush into the top-left corner, as on Up Next: the marker first, the
            // search card's watched tab under it. Only the stack's last tab rounds its corner.
            Column(Modifier.align(Alignment.TopStart)) {
                val tabShape = RoundedCornerShape(bottomEnd = 6.dp)
                marker?.let {
                    Text(
                        it.label,
                        style = MaterialTheme.typography.labelSmall,
                        fontWeight = FontWeight.SemiBold,
                        color = Color.White,
                        maxLines = 1,
                        modifier = Modifier
                            .then(if (watched) Modifier else Modifier.clip(tabShape))
                            .background(if (it == EpisodeMarker.SEASON_PREMIERE) SeasonPremiereBadge else FinaleBadge)
                            .padding(horizontal = 8.dp, vertical = 4.dp),
                    )
                }
                if (watched) {
                    Box(Modifier.clip(tabShape)) { MarkTab(DetailIcons.CheckBold, "Watched", WatchedItemColor) }
                }
            }
            userRating?.let {
                Text(
                    "$it",
                    style = MaterialTheme.typography.labelSmall,
                    fontWeight = FontWeight.SemiBold,
                    color = Color.White,
                    modifier = Modifier
                        .align(Alignment.TopEnd)
                        .padding(6.dp)
                        .clip(BadgeShape)
                        .background(HeartRed)
                        .padding(horizontal = 6.dp, vertical = 2.dp)
                        .semantics { contentDescription = "Your rating $it" },
                )
            }
            score?.let {
                val text = "%.1f".format(Locale.ENGLISH, it)
                Row(
                    Modifier.align(Alignment.BottomEnd)
                        .clip(RoundedCornerShape(topStart = 8.dp))
                        .background(Color.Black.copy(alpha = 0.6f))
                        .padding(horizontal = 7.dp, vertical = 3.dp)
                        .semantics(mergeDescendants = true) { contentDescription = "TMDB rating $text" },
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(3.dp),
                ) {
                    Icon(DetailIcons.Heart, null, Modifier.size(11.dp), tint = HeartRed)
                    Text(text, style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.SemiBold, color = Color.White)
                }
            }
        }
        Text(
            buildAnnotatedString {
                withStyle(SpanStyle(color = scheme.onSurfaceVariant)) { append("$code ") }
                append(name)
            },
            style = MaterialTheme.typography.labelLarge,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(top = 6.dp),
        )
        val facts = listOfNotNull(formatDay(airDate), runtime?.let { "$it min" }).joinToString(" · ")
        CardSub(facts.ifEmpty { "No air date yet" })
    }
}
