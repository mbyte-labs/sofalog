package ro.mbyte.sofalog.ui.details.parts

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedIconToggleButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Shadow
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import coil3.compose.AsyncImage
import ro.mbyte.sofalog.R
import ro.mbyte.sofalog.data.CopyKind
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.domain.CastMember
import ro.mbyte.sofalog.domain.MovieImage
import ro.mbyte.sofalog.domain.MovieVideo
import ro.mbyte.sofalog.domain.SimilarTitle
import ro.mbyte.sofalog.domain.Streaming
import ro.mbyte.sofalog.domain.watchAge
import ro.mbyte.sofalog.ui.common.RatingBar
import ro.mbyte.sofalog.ui.common.formatDay
import ro.mbyte.sofalog.ui.common.formatWatchDay
import ro.mbyte.sofalog.ui.common.formatWatchInstant
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.util.Locale

/*
 * The sections a details page is built from, shared by the movie and the show page and listed in
 * the web pages' order (MovieDetailsPage.svelte, ShowDetailsPage.svelte). They take plain values,
 * never a movie or a show, so both pages call the same code.
 * Each takes [page] — the screen's side gutter — rather than being padded from outside, so the
 * horizontal rails can run edge to edge and still start on the gutter.
 */

internal val TileShape = RoundedCornerShape(8.dp)
internal val ImdbYellow = Color(0xFFF5C518)
private val JellyfinPurple = Color(0xFFAA5CC3)
private val BluRayBlue = Color(0xFF0095D5)
internal val HeartRed = Color(0xFFF2555A)

/** A stable hue from a title, the web's `hueFor`: untitled artwork still gets its own colour. */
internal fun hueFor(text: String): Float {
    var hue = 0
    var i = 0
    while (i < text.length) {
        val cp = text.codePointAt(i)
        hue = (hue * 31 + cp) % 360
        i += Character.charCount(cp)
    }
    return hue.toFloat()
}

/** Up to two initials, a leading article dropped — the placeholder poster's text. */
internal fun initialsFor(title: String): String {
    val words = title.trim().split(Regex("\\s+")).filter { it.isNotEmpty() }
    val kept = if (words.size > 1 && words.first().lowercase() in setOf("the", "a", "an")) words.drop(1) else words
    return kept.take(2).joinToString("") { it.take(1).uppercase() }
}

internal fun hueBrush(hue: Float, saturation: Float, top: Float, bottom: Float) = Brush.linearGradient(
    listOf(Color.hsl(hue, saturation, top), Color.hsl(hue, saturation, bottom)),
)

internal fun UriHandler.open(url: String) {
    runCatching { openUri(url) }
}

// ---------------------------------------------------------------------------------- hero

/** The hero's credit line — "Directed by …", "Created by …": the lead-in dimmed, the names not. */
data class Byline(val lead: String, val names: String)

/**
 * The backdrop as a band that dissolves into the page, the poster overlapping its lower edge
 * beside the title block, then the facts as chips, the tagline and the overview. It draws under
 * the status bar. [bandHeight] is the screen's to decide: it knows how tall the window is.
 * [facts] is the one line under the title; [genres] is the catalog's comma-joined string.
 *
 * An episode has no poster of its own: with [poster] false the band is the page's one picture —
 * the caller hands the episode's still as [backdropPath] — and the title block takes the full
 * width under it.
 */
@Composable
fun DetailHero(
    title: String,
    posterPath: String?,
    backdropPath: String?,
    facts: List<String>,
    byline: Byline?,
    score: Double?,
    genres: String?,
    tagline: String?,
    overview: String?,
    noOverview: String,
    wide: Boolean,
    page: Dp,
    bandHeight: Dp,
    /** What this title belongs to — a season's show — said above it; [onOverline] makes it a way there. */
    overline: String? = null,
    onOverline: (() -> Unit)? = null,
    /** A second step of the same trail — an episode's season, after its show. */
    overline2: String? = null,
    onOverline2: (() -> Unit)? = null,
    /** False for a title with no poster of its own — an episode; [posterPath] then only backs a missing band. */
    poster: Boolean = true,
) {
    val hue = remember(title) { hueFor(title) }
    // How much of the poster lies over the band; the rest hangs below it, beside the title.
    // Without a poster only the trail above the title reaches into the band's faded foot.
    val overlap = if (!poster) 48.dp else if (wide) 150.dp else 76.dp
    Box(Modifier.fillMaxWidth()) {
        HeroBackdrop(posterPath, backdropPath, hue, wide, Modifier.fillMaxWidth().height(bandHeight))
        Column(
            Modifier.padding(start = page, end = page, top = bandHeight - overlap, bottom = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (!poster) {
                Column(
                    Modifier.fillMaxWidth().widthIn(max = if (wide) 840.dp else Dp.Unspecified),
                    verticalArrangement = Arrangement.spacedBy(if (wide) 10.dp else 4.dp),
                ) {
                    HeroTitle(title, facts, byline, wide, overline, onOverline, overline2, onOverline2)
                }
                HeroChips(score, genres)
                HeroDescription(tagline, overview, noOverview, clamp = !wide, if (wide) Modifier.widthIn(max = 640.dp) else Modifier)
            } else if (wide) {
                Row(horizontalArrangement = Arrangement.spacedBy(32.dp)) {
                    HeroPoster(title, posterPath, hue, Modifier.width(220.dp))
                    Column(Modifier.weight(1f).padding(top = overlap + 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        HeroTitle(title, facts, byline, wide = true, overline, onOverline)
                        HeroChips(score, genres)
                        HeroDescription(tagline, overview, noOverview, clamp = false, Modifier.padding(top = 6.dp).widthIn(max = 640.dp))
                    }
                }
            } else {
                Row(horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.Bottom) {
                    HeroPoster(title, posterPath, hue, Modifier.width(112.dp))
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        HeroTitle(title, facts, byline, wide = false, overline, onOverline)
                    }
                }
                HeroChips(score, genres)
                byline?.let { BylineText(it) }
                HeroDescription(tagline, overview, noOverview, clamp = true, Modifier)
            }
        }
    }
}

/** The title's own backdrop; without one, the blurred poster over the title's hue. */
@Composable
private fun HeroBackdrop(posterPath: String?, backdropPath: String?, hue: Float, wide: Boolean, modifier: Modifier) {
    val background = MaterialTheme.colorScheme.background
    Box(modifier.clipToBounds().background(hueBrush(hue, 0.38f, 0.18f, 0.08f))) {
        val backdrop = if (wide) TmdbClient.backdropFullUrl(backdropPath) else TmdbClient.backdropUrl(backdropPath)
        if (backdrop != null) {
            AsyncImage(backdrop, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
        } else {
            TmdbClient.posterUrl(posterPath)?.let {
                AsyncImage(
                    model = it,
                    contentDescription = null,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize().blur(40.dp).alpha(0.6f),
                )
            }
        }
        // Dimmed at the top for the status bar, clear through the middle, gone into the page below.
        Box(
            Modifier.fillMaxSize().background(
                Brush.verticalGradient(
                    0f to background.copy(alpha = 0.45f),
                    0.3f to background.copy(alpha = 0.05f),
                    0.5f to background.copy(alpha = 0.15f),
                    0.82f to background.copy(alpha = 0.8f),
                    1f to background,
                ),
            ),
        )
    }
}

@Composable
private fun HeroPoster(title: String, posterPath: String?, hue: Float, modifier: Modifier) {
    Box(
        modifier
            .aspectRatio(2f / 3f)
            .shadow(12.dp, TileShape)
            .clip(TileShape)
            .background(hueBrush(hue, 0.42f, 0.32f, 0.14f))
            .border(1.dp, Color.White.copy(alpha = 0.14f), TileShape),
        contentAlignment = Alignment.Center,
    ) {
        val url = TmdbClient.posterUrl(posterPath)
        if (url == null) {
            Text(initialsFor(title), fontSize = 36.sp, fontWeight = FontWeight.Bold, color = Color.White.copy(alpha = 0.55f))
        } else {
            AsyncImage(url, title, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
        }
    }
}

private val HeroShadow = Shadow(Color.Black.copy(alpha = 0.6f), Offset(0f, 2f), 8f)

/** The title and the one line of facts under it. A wide screen has room for the byline there. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun HeroTitle(
    title: String,
    facts: List<String>,
    byline: Byline?,
    wide: Boolean,
    overline: String?,
    onOverline: (() -> Unit)?,
    overline2: String? = null,
    onOverline2: (() -> Unit)? = null,
) {
    if (overline != null && overline2 != null) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            // The show's name gives way first: the season's is short and is the nearer step.
            Overline(overline, onOverline, Modifier.weight(1f, fill = false))
            Text(
                "›",
                style = MaterialTheme.typography.titleSmall.copy(shadow = HeroShadow),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(end = 8.dp),
            )
            Overline(overline2, onOverline2)
        }
    } else {
        (overline ?: overline2)?.let { Overline(it, if (overline != null) onOverline else onOverline2) }
    }
    Text(
        title,
        style = (if (wide) MaterialTheme.typography.displaySmall else MaterialTheme.typography.headlineSmall).copy(
            fontWeight = FontWeight.Bold,
            shadow = HeroShadow,
        ),
        maxLines = 3,
        overflow = TextOverflow.Ellipsis,
    )
    if (wide) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (facts.isNotEmpty()) HeroFacts(facts.joinToString("  ·  "))
            byline?.let {
                if (facts.isNotEmpty()) HeroFacts("·")
                BylineText(it)
            }
        }
    } else if (facts.isNotEmpty()) {
        HeroFacts(facts.joinToString("  ·  "))
    }
}

@Composable
private fun Overline(text: String, onClick: (() -> Unit)?, modifier: Modifier = Modifier) {
    Text(
        text,
        style = MaterialTheme.typography.titleSmall.copy(shadow = HeroShadow),
        color = MaterialTheme.colorScheme.primary,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = modifier
            .clip(RoundedCornerShape(6.dp))
            .clickable(enabled = onClick != null, role = Role.Button, onClickLabel = "Open $text") { onClick?.invoke() }
            .heightIn(min = 32.dp)
            .padding(end = 8.dp)
            .wrapContentHeight(Alignment.CenterVertically),
    )
}

@Composable
private fun HeroFacts(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodyMedium.copy(shadow = HeroShadow),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

@Composable
private fun BylineText(byline: Byline) {
    Text(
        buildAnnotatedString {
            withStyle(SpanStyle(color = MaterialTheme.colorScheme.onSurfaceVariant)) { append("${byline.lead} ") }
            append(byline.names)
        },
        style = MaterialTheme.typography.bodyMedium,
    )
}

/** TMDB's score, then a chip per genre. Nothing at all when the title has neither. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun HeroChips(voteAverage: Double?, genreList: String?) {
    val score = voteAverage?.takeIf { it > 0 }?.let { "%.1f".format(Locale.ENGLISH, it) }
    val genres = genreList?.split(", ")?.filter { it.isNotBlank() }.orEmpty()
    if (score == null && genres.isEmpty()) return
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        score?.let {
            HeroChip(Modifier.semantics(mergeDescendants = true) { contentDescription = "TMDB rating $it" }) {
                Icon(DetailIcons.Star, null, Modifier.size(14.dp), tint = ImdbYellow)
                Text(it, style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold)
            }
        }
        genres.forEach { genre ->
            HeroChip { Text(genre, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
    }
}

@Composable
private fun HeroChip(modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Surface(
        shape = CircleShape,
        color = MaterialTheme.colorScheme.surfaceContainer,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
        modifier = modifier,
    ) {
        Row(
            Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) { content() }
    }
}

private const val OVERVIEW_CLAMPED = 4

/** The tagline and the overview. [clamp] folds a long overview behind a "Read more". */
@Composable
private fun HeroDescription(tagline: String?, overview: String?, noOverview: String, clamp: Boolean, modifier: Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        tagline?.let {
            Text(it, style = MaterialTheme.typography.bodyMedium, fontStyle = FontStyle.Italic)
        }
        if (overview == null) {
            Note(noOverview)
            return@Column
        }
        var expanded by rememberSaveable { mutableStateOf(false) }
        // Whether the clamped text was cut — a short overview gets no button.
        var cut by remember { mutableStateOf(false) }
        Text(
            overview,
            style = MaterialTheme.typography.bodyLarge.copy(letterSpacing = 0.sp),
            maxLines = if (clamp && !expanded) OVERVIEW_CLAMPED else Int.MAX_VALUE,
            overflow = TextOverflow.Ellipsis,
            onTextLayout = { if (!expanded) cut = it.hasVisualOverflow },
        )
        if (clamp && (cut || expanded)) {
            TextButton(
                onClick = { expanded = !expanded },
                contentPadding = PaddingValues(horizontal = 4.dp),
                // Pulled up and left so the label lines up with the text it belongs to.
                modifier = Modifier.padding(top = 0.dp).offset(x = (-4).dp, y = (-8).dp),
            ) { Text(if (expanded) "Show less" else "Read more") }
        }
    }
}

/** "2h 12m", "45m", "2h" — a runtime in minutes, read as a length rather than a count. */
internal fun formatRuntime(minutes: Int): String {
    val hours = minutes / 60
    val rest = minutes % 60
    return when {
        hours == 0 -> "${rest}m"
        rest == 0 -> "${hours}h"
        else -> "${hours}h ${rest}m"
    }
}

// ------------------------------------------------------------------------------ scaffolding

@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier) {
    Text(
        text.uppercase(),
        style = MaterialTheme.typography.labelMedium.copy(letterSpacing = 1.4.sp, fontWeight = FontWeight.SemiBold),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = modifier,
    )
}

@Composable
internal fun SubLabel(text: String) {
    Text(
        text.uppercase(),
        style = MaterialTheme.typography.labelSmall.copy(letterSpacing = 1.2.sp),
        color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.75f),
    )
}

@Composable
internal fun Note(text: String, modifier: Modifier = Modifier) {
    Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = modifier)
}

/** A section whose content sits inside the gutter. Rails use [RailSection] instead. */
@Composable
fun Section(label: String, page: Dp, content: @Composable () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(start = page, end = page, top = 28.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        SectionLabel(label)
        content()
    }
}

@Composable
internal fun RailSection(label: String, page: Dp, content: @Composable () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(top = 28.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        SectionLabel(label, Modifier.padding(horizontal = page))
        content()
    }
}

/** Two blocks side by side on a wide screen, stacked on a narrow one. */
@Composable
internal fun Paired(wide: Boolean, first: @Composable () -> Unit, second: @Composable () -> Unit) {
    if (wide) {
        Row(horizontalArrangement = Arrangement.spacedBy(48.dp)) {
            Box(Modifier.weight(1f)) { first() }
            Box(Modifier.weight(1f)) { second() }
        }
    } else {
        Column(verticalArrangement = Arrangement.spacedBy(20.dp)) { first(); second() }
    }
}

// ------------------------------------------------------------------------------- details

/** One labelled fact of the Details section. */
data class DetailFact(val label: String, val value: String, val note: String? = null)

/** TMDB's popularity with the day it was read — it moves daily, so the number alone would mislead. */
internal fun popularityFact(popularity: Double?, fetchedAt: String): DetailFact? = popularity?.let {
    DetailFact("Popularity", "%.1f".format(Locale.ENGLISH, it), fetchedDay(fetchedAt)?.let { d -> "as of $d" })
}

/** Outbound links, then the facts TMDB publishes about the production. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DetailsSection(
    tmdbUrl: String,
    imdbId: String?,
    homepage: String?,
    trailerUrl: String?,
    facts: List<DetailFact>,
    wide: Boolean,
    page: Dp,
) {
    val uri = LocalUriHandler.current
    Section("Details", page) {
        Paired(
            wide && facts.isNotEmpty(),
            first = {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    LinkPill("TMDB", onClick = { uri.open(tmdbUrl) }) {
                        Image(painterResource(R.drawable.tmdb_logo), contentDescription = null, modifier = Modifier.height(12.dp))
                    }
                    imdbId?.let { id ->
                        LinkPill("IMDb", onClick = { uri.open("https://www.imdb.com/title/$id/") }) {
                            Icon(DetailIcons.Imdb, null, Modifier.size(22.dp), tint = ImdbYellow)
                        }
                    }
                    homepage?.let { url ->
                        LinkPill("Website", onClick = { uri.open(url) }) { Icon(DetailIcons.Globe, null, Modifier.size(18.dp)) }
                    }
                    trailerUrl?.let { url ->
                        LinkPill("Trailer", onClick = { uri.open(url) }) { Icon(DetailIcons.Play, null, Modifier.size(20.dp)) }
                    }
                }
            },
            second = {
                if (facts.isNotEmpty()) {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(32.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                        facts.forEach { Fact(it.label, it.value, it.note) }
                    }
                }
            },
        )
    }
}

@Composable
private fun LinkPill(label: String, onClick: () -> Unit, icon: @Composable () -> Unit) {
    Surface(
        onClick = onClick,
        shape = CircleShape,
        color = MaterialTheme.colorScheme.surfaceContainer,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
        modifier = Modifier.height(48.dp).semantics { contentDescription = "Open $label" },
    ) {
        Row(
            Modifier.padding(horizontal = 18.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            icon()
            // The TMDB pill's mark is the wordmark itself; naming it twice would only crowd it.
            if (label != "TMDB") Text(label, style = MaterialTheme.typography.labelLarge)
        }
    }
}

@Composable
internal fun Fact(label: String, value: String, note: String? = null) {
    Column {
        SubLabel(label)
        Text(value, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
        note?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

internal fun languageName(code: String): String =
    Locale.forLanguageTag(code).getDisplayLanguage(Locale.ENGLISH).ifBlank { code }

/** "$63M", "$1.2B" — the web's compact notation, at most one fraction digit. */
internal fun compactUsd(amount: Long): String {
    val (value, suffix) = when {
        amount >= 1_000_000_000 -> amount / 1e9 to "B"
        amount >= 1_000_000 -> amount / 1e6 to "M"
        amount >= 1_000 -> amount / 1e3 to "K"
        else -> return "$$amount"
    }
    val text = "%.1f".format(Locale.ENGLISH, value).removeSuffix(".0")
    return "$$text$suffix"
}

internal fun fetchedDay(fetchedAt: String): String? = runCatching {
    formatDay(LocalDate.ofInstant(Instant.parse(fetchedAt), ZoneId.systemDefault()).toString())
}.getOrNull()

// ------------------------------------------------------------------------------- ratings

@Composable
fun RatingsSection(
    voteAverage: Double?,
    votes: Long?,
    userRating: Int?,
    /** Null for a title that is not itself rated — a season: [extra] then takes the bar's place. */
    onRate: ((Int?) -> Unit)?,
    wide: Boolean,
    page: Dp,
    /** A further block under the pair — the show page's episode average. */
    extra: (@Composable () -> Unit)? = null,
    /** Said under the score instead of the vote count, where TMDB publishes none. */
    scoreCaption: String? = null,
) {
    Column(Modifier.fillMaxWidth().padding(start = page, end = page, top = 28.dp)) {
        Paired(
            wide,
            first = {
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    SectionLabel("Ratings")
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                        Image(painterResource(R.drawable.tmdb_logo), contentDescription = "TMDB", modifier = Modifier.height(14.dp))
                        Column {
                            Text(
                                voteAverage?.let { "%.1f".format(Locale.ENGLISH, it) } ?: "—",
                                fontSize = 26.sp,
                                fontWeight = FontWeight.Bold,
                            )
                            Text(
                                scoreCaption ?: votes?.let { "%,d votes".format(Locale.ENGLISH, it) } ?: "no votes",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            },
            second = {
                if (onRate == null) {
                    extra?.invoke()
                } else {
                    Column(Modifier.widthIn(max = 480.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        SectionLabel("Your rating")
                        RatingBar(userRating, onRate)
                        Note(if (userRating != null) "Your rating: $userRating/10" else "What did you think? Tap or slide to rate")
                    }
                }
            },
        )
        if (onRate != null && extra != null) {
            Spacer(Modifier.height(20.dp))
            extra()
        }
    }
}

// ----------------------------------------------------------------------------- watch now

/**
 * One "Your copies" tile. A movie is owned on a kind or it is not; a show is owned per season, so
 * its tile can be [partial] — some seasons, not all — and says how many in [caption].
 */
data class CopyTileState(
    val kind: CopyKind,
    val owned: Boolean,
    val partial: Boolean = false,
    val caption: String? = null,
)

/** "Your copies" — the formats the user owns this on — and where it streams in one country. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun WatchNowSection(
    tiles: List<CopyTileState>,
    streaming: Streaming?,
    /** What the page is about, for the empty streaming note: "movie" or "show". */
    subject: String,
    enabled: Boolean,
    onCopyTap: (CopyKind) -> Unit,
    wide: Boolean,
    page: Dp,
    /** Whose copies the tiles are, where they are not the page's own — an episode shows its season's. */
    copiesLabel: String = "Your copies",
) {
    Section("Watch now", page) {
        Paired(
            wide && streaming != null,
            first = {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    SubLabel(copiesLabel)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        tiles.forEach { tile -> CopyTile(tile, enabled = enabled, onClick = { onCopyTap(tile.kind) }) }
                    }
                }
            },
            second = { if (streaming != null) StreamingGroup(streaming, subject) },
        )
    }
}

@Composable
private fun CopyTile(tile: CopyTileState, enabled: Boolean, onClick: () -> Unit) {
    val scheme = MaterialTheme.colorScheme
    val kind = tile.kind
    val owned = tile.owned
    val described = when {
        !owned -> "Not owned on ${kind.label}"
        tile.caption != null -> "Owned on ${kind.label}, ${tile.caption}"
        else -> "Owned on ${kind.label}"
    }
    Column(
        Modifier
            .width(84.dp)
            .clip(TileShape)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .semantics { contentDescription = described }
            .padding(vertical = 6.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Box(
            Modifier
                .size(52.dp)
                .alpha(if (owned) 1f else 0.5f)
                .clip(TileShape)
                // Part of a show reads as an outline: lit, but not filled in.
                .background(if (owned && !tile.partial) scheme.primaryContainer else scheme.surfaceContainer)
                .border(if (tile.partial) 1.5.dp else 1.dp, if (owned) scheme.primary else scheme.outlineVariant, TileShape),
            contentAlignment = Alignment.Center,
        ) {
            val plain = when {
                !owned -> scheme.onSurfaceVariant
                tile.partial -> scheme.primary
                else -> scheme.onPrimaryContainer
            }
            when (kind) {
                CopyKind.JELLYFIN -> Icon(DetailIcons.Jellyfin, null, Modifier.size(28.dp), tint = if (owned) JellyfinPurple else plain)
                CopyKind.DVD -> Wordmark("DVD", plain)
                CopyKind.BLURAY -> Icon(DetailIcons.Disc, null, Modifier.size(28.dp), tint = if (owned) BluRayBlue else plain)
                CopyKind.UHD -> Wordmark("4K", plain)
                CopyKind.PHYSICAL -> Icon(DetailIcons.Box, null, Modifier.size(26.dp), tint = plain)
                CopyKind.DIGITAL -> Icon(DetailIcons.Drive, null, Modifier.size(26.dp), tint = plain)
            }
        }
        Text(
            kind.label,
            style = MaterialTheme.typography.labelSmall,
            fontWeight = FontWeight.SemiBold,
            color = if (owned) scheme.onSurface else scheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
            maxLines = 2,
        )
        tile.caption?.let {
            Text(
                it,
                style = MaterialTheme.typography.labelSmall,
                color = scheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
                maxLines = 2,
            )
        }
    }
}

@Composable
private fun Wordmark(text: String, color: Color) {
    Text(text, fontSize = 16.sp, fontWeight = FontWeight.ExtraBold, fontStyle = FontStyle.Italic, color = color)
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun StreamingGroup(streaming: Streaming, subject: String) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        SubLabel("Streaming")
        if (streaming.providers.isEmpty()) {
            Note("This $subject isn't on any streaming service in ${streaming.region}.")
        } else {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                streaming.providers.forEach { provider ->
                    Column(
                        Modifier.width(84.dp).padding(vertical = 6.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                        verticalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        val hue = remember(provider.name) { hueFor(provider.name) }
                        Box(
                            Modifier.size(52.dp).clip(TileShape).background(hueBrush(hue, 0.4f, 0.32f, 0.14f)),
                            contentAlignment = Alignment.Center,
                        ) {
                            val logo = TmdbClient.logoUrl(provider.logoPath)
                            if (logo == null) {
                                Text(initialsFor(provider.name), fontWeight = FontWeight.Bold, color = Color.White.copy(alpha = 0.7f))
                            } else {
                                AsyncImage(logo, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
                            }
                        }
                        Text(
                            provider.name,
                            style = MaterialTheme.typography.labelSmall,
                            fontWeight = FontWeight.SemiBold,
                            textAlign = TextAlign.Center,
                            maxLines = 2,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Text(
                            provider.offer.label,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
        val linkStyle = TextLinkStyles(SpanStyle(color = MaterialTheme.colorScheme.primary))
        Text(
            buildAnnotatedString {
                append("Availability in ${streaming.region}. Powered by ")
                if (streaming.link != null) {
                    withLink(LinkAnnotation.Url(streaming.link, linkStyle)) { append("JustWatch") }
                } else {
                    append("JustWatch")
                }
                append(", via TMDB.")
            },
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

// ------------------------------------------------------------------------------- history

/** A log longer than this by more than one row starts collapsed to this many. */
private const val HISTORY_COLLAPSED = 3

/** How many more entries one tap unfolds once a log is long. */
private const val HISTORY_STEP = 20

/**
 * One row of a page's own watch timeline. A movie's entries are all the movie, so they carry no
 * [title]; a show's name the episode each one is a watch of.
 */
data class TimelineEntry(val id: Long, val watchedAt: String, val title: String? = null)

/**
 * The watch log with everything that belongs to it: a summary, the timeline — newest on top, a
 * tap on an entry asks to remove it — and the page's main button with the two list toggles. A
 * phone stacks them; a wide screen puts the summary and the buttons beside the timeline.
 *
 * [entries] is newest first. [watched] picks the main button's wording: it is the page's to say,
 * because a show with a history is not thereby a watched show.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun HistorySection(
    entries: List<TimelineEntry>,
    watched: Boolean,
    watchlisted: Boolean,
    favorited: Boolean,
    enabled: Boolean,
    onEntryTap: (TimelineEntry) -> Unit,
    /** Each of the three is null where the page has no such action — a season is not listed. */
    onLogWatch: (() -> Unit)?,
    onWatchlist: ((Boolean) -> Unit)?,
    onFavorite: ((Boolean) -> Unit)?,
    wide: Boolean,
    page: Dp,
    emptyNote: String = "Not watched yet.",
    /** Said under the buttons — what a first action on an untracked title sets off. */
    hint: String? = null,
) {
    val zone = remember { ZoneId.systemDefault() }
    val today = LocalDate.now(zone)
    // Entries that name what was watched are plays of many things, not viewings of one.
    val plays = entries.any { it.title != null }
    val hasButtons = onLogWatch != null || onWatchlist != null || onFavorite != null
    val actions = @Composable {
        if (hasButtons || hint != null) {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (hasButtons) HistoryActions(watched, watchlisted, favorited, enabled, onLogWatch, onWatchlist, onFavorite)
                hint?.let { Note(it, Modifier.widthIn(max = 440.dp)) }
            }
        }
    }
    Column(Modifier.fillMaxWidth().padding(start = page, end = page, top = 28.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (entries.isEmpty()) {
            SectionLabel("History")
            Note(emptyNote)
            actions()
            return@Column
        }
        val lastAge = watchAge(entries.first().watchedAt, today, zone)
        val count = if (plays) episodesWatched(entries.size) else timesWatched(entries.size)
        if (wide) {
            SectionLabel("History")
            Paired(
                wide = true,
                first = {
                    Column(verticalArrangement = Arrangement.spacedBy(20.dp)) {
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(32.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                            Fact("Watched", count.removePrefix("Watched ").replaceFirstChar { it.uppercase() })
                            lastAge?.let { Fact("Last", it) }
                            if (entries.size > 1) Fact("First", formatWatchDay(entries.last().watchedAt))
                        }
                        actions()
                    }
                },
                second = { WatchTimeline(entries, today, zone, enabled, onEntryTap) },
            )
        } else {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                SectionLabel("History")
                Text(
                    when {
                        entries.size == 1 || lastAge == null -> count
                        plays -> "$count · last ${lastAge.lowercase(Locale.ENGLISH)}"
                        else -> "${entries.size} watches · last ${lastAge.lowercase(Locale.ENGLISH)}"
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            WatchTimeline(entries, today, zone, enabled, onEntryTap)
            actions()
        }
    }
}

private fun timesWatched(count: Int): String = when (count) {
    1 -> "Watched once"
    2 -> "Watched twice"
    else -> "Watched $count times"
}

private fun episodesWatched(count: Int): String = if (count == 1) "1 episode watch" else "$count episode watches"

/** The entries as one card, a dot per watch on a line that runs through them. */
@Composable
private fun WatchTimeline(
    entries: List<TimelineEntry>,
    today: LocalDate,
    zone: ZoneId,
    enabled: Boolean,
    onTap: (TimelineEntry) -> Unit,
) {
    // Hiding a single row behind a button that takes a row of its own would save nothing.
    var limit by rememberSaveable { mutableStateOf(HISTORY_COLLAPSED) }
    val shown = if (entries.size <= limit + 1) entries else entries.take(limit)
    val rail = MaterialTheme.colorScheme.outlineVariant
    val dot = MaterialTheme.colorScheme.primary
    Surface(
        shape = RoundedCornerShape(12.dp),
        color = MaterialTheme.colorScheme.surfaceContainer,
        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column {
            shown.forEachIndexed { index, entry ->
                key(entry.id) {
                    val oldest = index == entries.lastIndex
                    val age = watchAge(entry.watchedAt, today, zone)
                    // A titled entry leads with what was watched, so its instant moves to the second line.
                    val headline = entry.title ?: formatWatchInstant(entry.watchedAt)
                    val sub = if (entry.title != null) {
                        listOfNotNull(formatWatchInstant(entry.watchedAt), age).joinToString(" · ")
                    } else {
                        listOfNotNull(age, "first watch".takeIf { oldest && entries.size > 1 }).joinToString(" · ")
                    }
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable(enabled = enabled, role = Role.Button, onClickLabel = "Remove this watch") { onTap(entry) }
                            .heightIn(min = 60.dp)
                            .drawBehind {
                                val x = 24.dp.toPx()
                                val y = size.height / 2
                                val gap = 9.dp.toPx()
                                val stroke = 2.dp.toPx()
                                // The line stops short of each dot, and runs on past the last
                                // shown entry while older ones are still folded away.
                                if (index > 0) drawLine(rail, Offset(x, 0f), Offset(x, y - gap), stroke)
                                if (!oldest) drawLine(rail, Offset(x, y + gap), Offset(x, size.height), stroke)
                                drawCircle(dot, 5.dp.toPx(), Offset(x, y))
                            }
                            .padding(start = 48.dp, end = 12.dp, top = 10.dp, bottom = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text(headline, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                            if (sub.isNotEmpty()) {
                                Text(sub, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                        Icon(DetailIcons.More, null, Modifier.size(20.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
            if (shown.size < entries.size) {
                val hidden = entries.size - shown.size
                // A long log unfolds a step at a time: a show can hold hundreds of entries.
                val all = hidden <= HISTORY_STEP
                TextButton(
                    onClick = { limit = if (all) entries.size else limit + HISTORY_STEP },
                    modifier = Modifier.padding(start = 36.dp, bottom = 4.dp),
                ) { Text(if (all) "Show all ${entries.size}" else "Show $HISTORY_STEP more") }
            }
        }
    }
}

/** The page's main button, then the watchlist and favorite toggles. */
@Composable
private fun HistoryActions(
    watched: Boolean,
    watchlisted: Boolean,
    favorited: Boolean,
    enabled: Boolean,
    onLogWatch: (() -> Unit)?,
    onWatchlist: ((Boolean) -> Unit)?,
    onFavorite: ((Boolean) -> Unit)?,
) {
    val haptics = LocalHapticFeedback.current
    Row(
        Modifier.widthIn(max = 440.dp).fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        val button = Modifier.weight(1f).height(52.dp)
        if (onLogWatch != null) {
            if (watched) {
                OutlinedButton(onClick = onLogWatch, enabled = enabled, modifier = button) { Text("Log another watch") }
            } else {
                Button(onClick = onLogWatch, enabled = enabled, modifier = button) { Text("Mark As Watched") }
            }
        }
        if (onWatchlist != null) Toggle(
            checked = watchlisted,
            enabled = enabled,
            on = DetailIcons.Bookmark,
            off = DetailIcons.BookmarkOutline,
            label = if (watchlisted) "Remove from watchlist" else "Add to watchlist",
            tint = MaterialTheme.colorScheme.primary,
        ) {
            haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove)
            onWatchlist(it)
        }
        if (onFavorite != null) Toggle(
            checked = favorited,
            enabled = enabled,
            on = DetailIcons.Heart,
            off = DetailIcons.HeartOutline,
            label = if (favorited) "Remove from favorites" else "Add to favorites",
            tint = HeartRed,
        ) {
            haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove)
            onFavorite(it)
        }
    }
}

@Composable
private fun Toggle(
    checked: Boolean,
    enabled: Boolean,
    on: ImageVector,
    off: ImageVector,
    label: String,
    tint: Color,
    onChange: (Boolean) -> Unit,
) {
    OutlinedIconToggleButton(
        checked = checked,
        onCheckedChange = onChange,
        enabled = enabled,
        modifier = Modifier.size(52.dp),
        colors = IconButtonDefaults.outlinedIconToggleButtonColors(checkedContainerColor = tint.copy(alpha = 0.18f)),
        border = BorderStroke(1.5.dp, tint.copy(alpha = if (enabled) 1f else 0.4f)),
    ) {
        Icon(if (checked) on else off, contentDescription = label, tint = if (enabled) tint else tint.copy(alpha = 0.4f))
    }
}

// ---------------------------------------------------------------------------------- cast

private const val CAST_COLLAPSED = 20

@Composable
fun CastSection(title: String, cast: List<CastMember>, emptyNote: String, wide: Boolean, page: Dp, label: String = "Cast") {
    RailSection(label, page) {
        if (cast.isEmpty()) {
            Note(emptyNote, Modifier.padding(horizontal = page))
            return@RailSection
        }
        var expanded by rememberSaveable { mutableStateOf(false) }
        val shown = if (expanded) cast else cast.take(CAST_COLLAPSED)
        val hidden = cast.size - shown.size
        val base = remember(title) { hueFor(title) }
        val card = Modifier.width(if (wide) 132.dp else 108.dp)
        LazyRow(contentPadding = PaddingValues(horizontal = page), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            itemsIndexed(shown) { index, member ->
                Column(card) {
                    Box(
                        Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(TileShape)
                            .background(hueBrush((base + index * 40) % 360, 0.2f, 0.26f, 0.13f)),
                        contentAlignment = Alignment.Center,
                    ) {
                        val photo = TmdbClient.profileUrl(member.profilePath)
                        if (photo == null) {
                            Icon(DetailIcons.Person, null, Modifier.size(42.dp), tint = Color.White.copy(alpha = 0.25f))
                        } else {
                            AsyncImage(photo, member.name, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
                        }
                    }
                    CardTitle(member.name)
                    member.character?.let { CardSub(it) }
                    member.episodes?.let { CardSub(if (it == 1) "1 episode" else "$it episodes") }
                }
            }
            if (hidden > 0) {
                item {
                    Surface(
                        onClick = { expanded = true },
                        shape = TileShape,
                        color = MaterialTheme.colorScheme.surfaceContainer,
                        border = BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
                        modifier = card.aspectRatio(2f / 3f).semantics { contentDescription = "Show $hidden more cast members" },
                    ) {
                        Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                            Text("+$hidden", fontSize = 22.sp, fontWeight = FontWeight.Bold)
                            Text("more", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
            }
        }
    }
}

@Composable
internal fun CardTitle(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.labelLarge,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.padding(top = 6.dp),
    )
}

@Composable
internal fun CardSub(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
    )
}

// --------------------------------------------------------------------------------- media

private enum class MediaTab(val label: String) { BACKDROPS("Backdrops"), VIDEOS("Videos"), POSTERS("Posters") }

/** Backdrops, videos and posters behind tabs. An image opens the lightbox; a video opens YouTube. */
@Composable
fun MediaSection(
    videos: List<MovieVideo>,
    backdrops: List<MovieImage>,
    posters: List<MovieImage>,
    emptyNote: String,
    wide: Boolean,
    page: Dp,
    onOpenImages: (List<String>, Int) -> Unit,
) {
    val uri = LocalUriHandler.current
    RailSection("Media", page) {
        val tabs = listOfNotNull(
            (MediaTab.BACKDROPS to backdrops.size).takeIf { it.second > 0 },
            (MediaTab.VIDEOS to videos.size).takeIf { it.second > 0 },
            (MediaTab.POSTERS to posters.size).takeIf { it.second > 0 },
        )
        if (tabs.isEmpty()) {
            Note(emptyNote, Modifier.padding(horizontal = page))
            return@RailSection
        }
        var chosen by rememberSaveable { mutableStateOf(tabs.first().first) }
        val tab = if (tabs.any { it.first == chosen }) chosen else tabs.first().first
        Row(
            Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = page),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            tabs.forEach { (option, count) ->
                FilterChip(
                    selected = tab == option,
                    onClick = { chosen = option },
                    label = { Text("${option.label}  $count") },
                    modifier = Modifier.defaultMinSize(minHeight = 40.dp),
                )
            }
        }
        val height = if (wide) 150.dp else 120.dp
        when (tab) {
            MediaTab.BACKDROPS -> ImageRail(backdrops, 16f / 9f, height, page, TmdbClient::backdropUrl) { index ->
                onOpenImages(backdrops.mapNotNull { TmdbClient.backdropFullUrl(it.path) }, index)
            }
            MediaTab.POSTERS -> ImageRail(posters, 2f / 3f, height + 40.dp, page, TmdbClient::posterUrl) { index ->
                onOpenImages(posters.mapNotNull { TmdbClient.posterFullUrl(it.path) }, index)
            }
            MediaTab.VIDEOS -> LazyRow(
                contentPadding = PaddingValues(horizontal = page),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                itemsIndexed(videos, key = { _, v -> v.key }) { _, video ->
                    VideoCard(video, Modifier.width(if (wide) 248.dp else 208.dp)) { uri.open(video.url) }
                }
            }
        }
    }
}

@Composable
private fun ImageRail(
    images: List<MovieImage>,
    fallbackAspect: Float,
    height: Dp,
    page: Dp,
    thumb: (String?) -> String?,
    onOpen: (Int) -> Unit,
) {
    LazyRow(contentPadding = PaddingValues(horizontal = page), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        itemsIndexed(images, key = { _, image -> image.path }) { index, image ->
            AsyncImage(
                model = thumb(image.path),
                contentDescription = "Image ${index + 1} of ${images.size}",
                contentScale = ContentScale.Crop,
                modifier = Modifier
                    .height(height)
                    .aspectRatio(image.aspect(fallbackAspect))
                    .clip(TileShape)
                    .background(MaterialTheme.colorScheme.surfaceContainer)
                    .clickable { onOpen(index) },
            )
        }
    }
}

@Composable
private fun VideoCard(video: MovieVideo, modifier: Modifier, onClick: () -> Unit) {
    Column(modifier.clip(TileShape).clickable(role = Role.Button, onClick = onClick)) {
        Box(
            Modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(TileShape).background(MaterialTheme.colorScheme.surfaceContainer),
            contentAlignment = Alignment.Center,
        ) {
            AsyncImage(video.thumbUrl, null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
            Box(Modifier.size(44.dp).clip(CircleShape).background(Color.Black.copy(alpha = 0.55f)), contentAlignment = Alignment.Center) {
                Icon(DetailIcons.Play, "Play", Modifier.size(26.dp), tint = Color.White)
            }
        }
        CardTitle(video.name.ifBlank { video.type })
        Row(
            Modifier.padding(top = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            val scheme = MaterialTheme.colorScheme
            Text(
                video.type,
                style = MaterialTheme.typography.labelSmall,
                color = if (video.official) scheme.onPrimaryContainer else scheme.onSurfaceVariant,
                modifier = Modifier
                    .clip(RoundedCornerShape(6.dp))
                    .background(if (video.official) scheme.primaryContainer else scheme.surfaceContainerHigh)
                    .padding(horizontal = 6.dp, vertical = 2.dp),
            )
            formatDay(video.publishedDay)?.let {
                Text(it, style = MaterialTheme.typography.labelSmall, color = scheme.onSurfaceVariant)
            }
        }
    }
}

// ------------------------------------------------------------------------------- similar

@Composable
fun SimilarSection(
    label: String,
    emptyNote: String,
    similar: List<SimilarTitle>,
    wide: Boolean,
    page: Dp,
    onOpen: (Long) -> Unit,
) {
    RailSection(label, page) {
        if (similar.isEmpty()) {
            Note(emptyNote, Modifier.padding(horizontal = page))
            return@RailSection
        }
        LazyRow(contentPadding = PaddingValues(horizontal = page), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            itemsIndexed(similar, key = { _, s -> s.tmdbId }) { _, item ->
                Column(
                    Modifier.width(if (wide) 132.dp else 108.dp).clip(TileShape)
                        .clickable(role = Role.Button) { onOpen(item.tmdbId) },
                ) {
                    val hue = remember(item.title) { hueFor(item.title) }
                    Box(
                        Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(TileShape).background(hueBrush(hue, 0.42f, 0.32f, 0.14f)),
                        contentAlignment = Alignment.Center,
                    ) {
                        val poster = TmdbClient.posterUrl(item.posterPath)
                        if (poster == null) {
                            Text(initialsFor(item.title), fontSize = 24.sp, fontWeight = FontWeight.Bold, color = Color.White.copy(alpha = 0.5f))
                        } else {
                            AsyncImage(poster, item.title, Modifier.fillMaxSize(), contentScale = ContentScale.Crop)
                        }
                        item.rating?.let { rating ->
                            Row(
                                Modifier.align(Alignment.BottomEnd)
                                    .clip(RoundedCornerShape(topStart = 8.dp))
                                    .background(Color.Black.copy(alpha = 0.6f))
                                    .padding(horizontal = 7.dp, vertical = 3.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(3.dp),
                            ) {
                                Icon(DetailIcons.Heart, null, Modifier.size(11.dp), tint = HeartRed)
                                Text(
                                    "%.1f".format(Locale.ENGLISH, rating),
                                    style = MaterialTheme.typography.labelSmall,
                                    fontWeight = FontWeight.SemiBold,
                                    color = Color.White,
                                )
                            }
                        }
                    }
                    CardTitle(item.title)
                    item.year?.let { CardSub(it) }
                }
            }
        }
        Spacer(Modifier.height(8.dp))
    }
}
