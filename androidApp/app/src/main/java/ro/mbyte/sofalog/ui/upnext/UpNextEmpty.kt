package ro.mbyte.sofalog.ui.upnext

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.slideInVertically
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import ro.mbyte.sofalog.R
import ro.mbyte.sofalog.ui.common.ActionCard
import ro.mbyte.sofalog.ui.details.parts.DetailIcons

/** How many pieces enter one after another: the logo, the text, then the three cards. */
private const val STEPS = 5
private const val STEP_MILLIS = 90L

/**
 * Up Next with nothing to offer: the app's mark, a line saying why the page is empty, and three
 * ways on. [hasHistory] tells a library that was never used from one that is caught up.
 *
 * The mark is the launcher's coloured foreground, drifting over a glow that breathes with it. The
 * pieces enter in turn the first time the page is shown; the step is saved, so a rotation or a
 * return from another page does not replay the entrance. No provider credit here — that lives on
 * the About page.
 */
@Composable
fun UpNextEmpty(
    hasHistory: Boolean,
    onSearch: () -> Unit,
    onTrending: () -> Unit,
    onSettings: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var step by rememberSaveable { mutableIntStateOf(0) }
    LaunchedEffect(Unit) {
        while (step < STEPS) {
            delay(STEP_MILLIS)
            step++
        }
    }

    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Column(
            Modifier
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp, vertical = 24.dp)
                .widthIn(max = 420.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Entering(step >= 1) { FloatingLogo() }
            Entering(step >= 2) {
                Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(
                        if (hasHistory) "All caught up" else "Your sofa is ready",
                        style = MaterialTheme.typography.headlineSmall,
                        textAlign = TextAlign.Center,
                    )
                    Text(
                        if (hasHistory) {
                            "Nothing left to watch right now. Time to find something new."
                        } else {
                            "Find a show, mark an episode watched, and your next one appears here."
                        },
                        style = MaterialTheme.typography.bodyMedium,
                        textAlign = TextAlign.Center,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }
            Spacer(Modifier.height(28.dp))
            Entering(step >= 3) {
                ActionCard(DetailIcons.Search, "Search", "Look up a movie or show by name", onSearch)
            }
            Spacer(Modifier.height(12.dp))
            Entering(step >= 4) {
                ActionCard(
                    DetailIcons.Trending,
                    "Trending on TMDB",
                    "See what everyone is watching this week",
                    onTrending,
                )
            }
            Spacer(Modifier.height(12.dp))
            Entering(step >= 5) {
                ActionCard(
                    DetailIcons.Settings,
                    "Settings",
                    "Your TMDB account, backups, time zone and country",
                    onSettings,
                )
            }
        }
    }
}

/** One piece of the entrance: fades in while rising a little. Its space is not held beforehand. */
@Composable
private fun Entering(visible: Boolean, content: @Composable () -> Unit) {
    AnimatedVisibility(
        visible = visible,
        enter = fadeIn(tween(350)) + slideInVertically(tween(350)) { it / 4 },
    ) { content() }
}

@Composable
private fun FloatingLogo() {
    val transition = rememberInfiniteTransition(label = "logo")
    // 0..1 and back, one way in 2.8 s: the mark rises as the glow swells.
    val phase by transition.animateFloat(
        initialValue = 0f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(tween(2800, easing = FastOutSlowInEasing), RepeatMode.Reverse),
        label = "phase",
    )
    val glow = MaterialTheme.colorScheme.primary
    Box(Modifier.size(200.dp), contentAlignment = Alignment.Center) {
        Box(
            Modifier
                .fillMaxSize()
                .graphicsLayer {
                    val scale = 0.85f + 0.15f * phase
                    scaleX = scale
                    scaleY = scale
                    alpha = 0.6f + 0.4f * phase
                }
                .background(Brush.radialGradient(listOf(glow.copy(alpha = 0.28f), Color.Transparent)), CircleShape),
        )
        Image(
            painterResource(R.drawable.ic_launcher_foreground),
            contentDescription = null,
            modifier = Modifier
                .size(200.dp)
                .graphicsLayer { translationY = (4f - 8f * phase).dp.toPx() },
        )
    }
}
