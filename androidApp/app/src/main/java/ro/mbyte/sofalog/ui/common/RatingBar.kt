package ro.mbyte.sofalog.ui.common

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectHorizontalDragGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The 1–10 rating picker — the web RatingBar's shape: the value as a number, then ten pill
 * segments filled up to it, all in the value's colour. Tap a segment or slide along the bar;
 * tapping the current value again, or Clear, unsets it. The parent owns the value.
 */
@Composable
fun RatingBar(value: Int?, onChange: (Int?) -> Unit, modifier: Modifier = Modifier) {
    val color = value?.let { ratingColor(it) } ?: MaterialTheme.colorScheme.onSurfaceVariant
    val current by rememberUpdatedState(value)
    val change by rememberUpdatedState(onChange)
    Column(modifier.fillMaxWidth()) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(value?.toString() ?: "–", fontSize = 28.sp, fontWeight = FontWeight.Bold, color = color)
            Text(
                " /10",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.weight(1f))
            // Kept in the layout when unset, so choosing a rating does not shift the bar.
            TextButton(onClick = { onChange(null) }, enabled = value != null) {
                Text(if (value != null) "Clear" else "")
            }
        }
        Row(
            Modifier
                .fillMaxWidth()
                .pointerInput(Unit) {
                    detectHorizontalDragGestures { pointer, _ ->
                        val next = (pointer.position.x / size.width * 10).toInt().coerceIn(0, 9) + 1
                        if (next != current) change(next)
                    }
                },
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            for (step in 1..10) {
                // The touch target is the full-height box; the pill is only what is drawn.
                Box(
                    Modifier
                        .weight(1f)
                        .height(44.dp)
                        .clickable(role = Role.Button) { onChange(if (value == step) null else step) }
                        .semantics { contentDescription = "Rate $step out of 10" },
                    contentAlignment = Alignment.Center,
                ) {
                    Box(
                        Modifier
                            .fillMaxWidth()
                            .height(14.dp)
                            .clip(CircleShape)
                            .background(
                                if (value != null && step <= value) color
                                else MaterialTheme.colorScheme.surfaceVariant,
                            ),
                    )
                }
            }
        }
    }
}

/** The web's `ratingColor` ramp: red 1–3, yellow 4–5, muted blue 6–7, the accent 8–9, green 10. */
@Composable
internal fun ratingColor(value: Int): Color = when {
    value >= 10 -> Color(0xFF3FB760)
    value >= 8 -> MaterialTheme.colorScheme.primary
    value >= 6 -> Color(0xFF6C8CD5)
    value >= 4 -> Color(0xFFEAB308)
    else -> Color(0xFFE8435A)
}
