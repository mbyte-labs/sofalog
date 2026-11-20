package ro.mbyte.sofalog.ui.stats

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.min
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.domain.StatsTitle
import ro.mbyte.sofalog.domain.titleHue
import ro.mbyte.sofalog.domain.titleInitials
import ro.mbyte.sofalog.ui.common.Poster
import ro.mbyte.sofalog.ui.common.PosterShape
import ro.mbyte.sofalog.ui.common.RatingBar
import ro.mbyte.sofalog.ui.common.ratingColor
import ro.mbyte.sofalog.ui.details.parts.Fact
import ro.mbyte.sofalog.ui.details.parts.SectionLabel
import ro.mbyte.sofalog.ui.theme.NeutralBadge
import ro.mbyte.sofalog.ui.theme.OnBadge
import ro.mbyte.sofalog.ui.theme.StatDown
import ro.mbyte.sofalog.ui.theme.StatUp

internal val CardShape = RoundedCornerShape(16.dp)

/** A title's name, or the stand-in for a stored row without one. */
internal fun StatsTitle.name(): String = title.ifBlank { if (kind == "movie") "A movie" else "A show" }

/** One section of the page: its label, an optional caption, then its content. */
@Composable
internal fun StatsSection(label: String, caption: String? = null, content: @Composable ColumnScope.() -> Unit) {
    Column(Modifier.fillMaxWidth().padding(top = 28.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
            SectionLabel(label)
            caption?.let { Caption(it) }
        }
        content()
    }
}

@Composable
internal fun Caption(text: String, modifier: Modifier = Modifier) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = modifier)
}

/** The page's card: a tonal surface, as Material 3 groups related content. */
@Composable
internal fun StatCard(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = CardShape,
        color = MaterialTheme.colorScheme.surfaceContainer,
    ) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp), content = content)
    }
}

/** A card's own heading, for a card that is one of several under a section. */
@Composable
internal fun CardTitle(text: String) {
    Text(text, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurface)
}

/** Two cards side by side on a wide screen, stacked on a narrow one. */
@Composable
internal fun TwoUp(wide: Boolean, first: @Composable () -> Unit, second: @Composable () -> Unit) {
    if (wide) {
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            Box(Modifier.weight(1f)) { first() }
            Box(Modifier.weight(1f)) { second() }
        }
    } else {
        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) { first(); second() }
    }
}

/** One headline number over its label. */
@Composable
internal fun StatTile(value: String, label: String, modifier: Modifier = Modifier) {
    Surface(modifier, shape = CardShape, color = MaterialTheme.colorScheme.surfaceContainer) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 14.dp)) {
            Text(value, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold, maxLines = 1)
            Text(
                label.uppercase(),
                style = MaterialTheme.typography.labelSmall.copy(letterSpacing = 1.sp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/** A row of labelled facts that wraps as the width runs out. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun Facts(facts: List<Pair<String, String>>, modifier: Modifier = Modifier) {
    FlowRow(
        modifier,
        horizontalArrangement = Arrangement.spacedBy(32.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        facts.forEach { (label, value) -> Fact(label, value) }
    }
}

/** A share of something as a filled track: [fraction] of the width in [color]. */
@Composable
internal fun StatMeter(fraction: Float, color: Color, modifier: Modifier = Modifier) {
    Box(
        modifier
            .fillMaxWidth()
            .height(8.dp)
            .clip(CircleShape)
            .background(MaterialTheme.colorScheme.surfaceContainerHighest),
    ) {
        Box(Modifier.fillMaxWidth(fraction.coerceIn(0f, 1f)).height(8.dp).clip(CircleShape).background(color))
    }
}

/** The colour of a signed difference: up reads green, down red, level stays the text's. */
@Composable
internal fun deltaColor(sign: Int): Color = when {
    sign > 0 -> StatUp
    sign < 0 -> StatDown
    else -> MaterialTheme.colorScheme.onSurface
}

/** A title with no artwork at all: its initials on a tint that is always the same for that name. */
@Composable
private fun InitialsArt(title: String, modifier: Modifier = Modifier) {
    val hue = titleHue(title).toFloat()
    Box(
        modifier.background(
            Brush.linearGradient(listOf(Color.hsl(hue, 0.40f, 0.32f), Color.hsl(hue, 0.45f, 0.14f))),
        ),
        contentAlignment = Alignment.Center,
    ) {
        Text(titleInitials(title), fontSize = 40.sp, fontWeight = FontWeight.Bold, color = Color.White.copy(alpha = 0.35f))
    }
}

/**
 * A title as a wide banner: its backdrop (else its poster, blurred; else its initials) under a
 * scrim carrying a badge, the name and a sub-line. The whole banner opens the title; [action], when
 * given, sits bottom-right and is its own target.
 */
@Composable
internal fun StatsBanner(
    title: StatsTitle,
    badge: String,
    onOpen: () -> Unit,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    action: (@Composable () -> Unit)? = null,
) {
    BoxWithConstraints(modifier.fillMaxWidth()) {
        // 16:9 on a phone, 2:1 beyond it — and never taller than a tablet can spare.
        val ratio = if (maxWidth < 600.dp) 16f / 9f else 2f
        val height = min(maxWidth / ratio, 300.dp)
        Box(
            Modifier
                .fillMaxWidth()
                .height(height)
                .clip(CardShape)
                .background(MaterialTheme.colorScheme.surfaceContainer)
                .clickable(onClickLabel = "Open ${title.name()}", role = Role.Button, onClick = onOpen),
        ) {
            val backdrop = TmdbClient.backdropUrl(title.backdropPath)
            val poster = TmdbClient.posterUrl(title.posterPath)
            when {
                backdrop != null -> AsyncImage(
                    model = backdrop,
                    contentDescription = null,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize(),
                )
                poster != null -> AsyncImage(
                    model = poster,
                    contentDescription = null,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize().blur(14.dp),
                )
                else -> InitialsArt(title.name(), Modifier.fillMaxSize())
            }
            Box(
                Modifier.fillMaxSize().background(
                    Brush.verticalGradient(
                        0f to Color.Transparent,
                        0.45f to Color.Black.copy(alpha = 0.25f),
                        1f to Color.Black.copy(alpha = 0.88f),
                    ),
                ),
            )
            Row(
                Modifier.align(Alignment.BottomStart).fillMaxWidth().padding(16.dp),
                verticalAlignment = Alignment.Bottom,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Column(Modifier.weight(1f)) {
                    Text(
                        badge.uppercase(),
                        style = MaterialTheme.typography.labelSmall.copy(letterSpacing = 1.sp, fontWeight = FontWeight.Bold),
                        color = MaterialTheme.colorScheme.primary,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Text(
                        title.name(),
                        style = MaterialTheme.typography.titleLarge,
                        color = Color.White,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    subtitle?.let {
                        Text(
                            it,
                            style = MaterialTheme.typography.bodySmall,
                            color = Color.White.copy(alpha = 0.75f),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                }
                action?.invoke()
            }
        }
    }
}

/**
 * A title as a list row: an optional rank, its poster, name and sub-line (with an optional meter
 * under them), then an optional figure and an optional action. The row opens the title.
 */
@Composable
internal fun StatsTitleRow(
    title: StatsTitle,
    onOpen: () -> Unit,
    modifier: Modifier = Modifier,
    rank: Int? = null,
    subtitle: String? = null,
    meter: Float? = null,
    figure: String? = null,
    figureColor: Color = MaterialTheme.colorScheme.primary,
    action: (@Composable () -> Unit)? = null,
) {
    Row(
        modifier.fillMaxWidth().heightIn(min = 56.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Row(
            Modifier
                .weight(1f)
                .clip(PosterShape)
                .clickable(onClickLabel = "Open ${title.name()}", role = Role.Button, onClick = onOpen),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            rank?.let {
                Text(
                    it.toString(),
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.Bold,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.widthIn(min = 20.dp),
                )
            }
            Poster(title.posterPath, titleInitials(title.name()), Modifier.width(44.dp))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    title.name(),
                    style = MaterialTheme.typography.bodyLarge,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                subtitle?.let {
                    Text(
                        it,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                meter?.let {
                    StatMeter(it, MaterialTheme.colorScheme.tertiary, Modifier.padding(top = 4.dp).widthIn(max = 260.dp))
                }
            }
        }
        figure?.let {
            Text(it, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold, color = figureColor)
        }
        action?.invoke()
    }
}

/** A title as a poster in the highest-rated rail, with the user's rating on its corner. */
@Composable
internal fun StatsPosterTile(title: StatsTitle, rating: Int, width: Dp, onOpen: () -> Unit) {
    Column(
        Modifier
            .width(width)
            .clip(PosterShape)
            .clickable(onClickLabel = "Open ${title.name()}", role = Role.Button, onClick = onOpen),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Box {
            Poster(title.posterPath, title.name(), Modifier.fillMaxWidth())
            Row(
                Modifier
                    .align(Alignment.BottomEnd)
                    .padding(6.dp)
                    .clip(CircleShape)
                    .background(NeutralBadge)
                    .padding(horizontal = 8.dp, vertical = 3.dp),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("♥", style = MaterialTheme.typography.labelMedium, color = ratingColor(rating))
                Text(rating.toString(), style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.Bold, color = OnBadge)
            }
        }
        Text(
            title.name(),
            style = MaterialTheme.typography.labelLarge,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        title.year?.let {
            Text(it.toString(), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/**
 * The rate sheet: one movie, the 1–10 picker, Save and Cancel. The parent owns the write — [onSave]
 * starts it, [busy] holds the sheet while it runs and [error] is its failure, shown in place.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun RateSheet(
    title: StatsTitle,
    busy: Boolean,
    error: String?,
    onSave: (Int) -> Unit,
    onDismiss: () -> Unit,
) {
    var rating by remember(title.kind, title.id) { mutableStateOf<Int?>(null) }
    ModalBottomSheet(
        // A write in flight finishes first: the sheet closes on its result, not under it.
        onDismissRequest = { if (!busy) onDismiss() },
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
    ) {
        Column(
            Modifier.fillMaxWidth().navigationBarsPadding().padding(start = 24.dp, end = 24.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("Rate", style = MaterialTheme.typography.titleLarge)
            Text(
                title.year?.let { "${title.name()} ($it)" } ?: title.name(),
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            SectionLabel("Your rating", Modifier.padding(top = 4.dp))
            RatingBar(rating, onChange = { if (!busy) rating = it })
            Caption(rating?.let { "Your rating: $it/10" } ?: "What did you think? Tap to rate")
            error?.let {
                Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.error)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                Button(
                    onClick = { rating?.let(onSave) },
                    enabled = !busy && rating != null,
                    modifier = Modifier.weight(1f),
                ) { Text(if (busy) "Saving…" else "Save") }
                OutlinedButton(onClick = onDismiss, enabled = !busy) { Text("Cancel") }
            }
            Spacer(Modifier.height(4.dp))
        }
    }
}
