package ro.mbyte.sofalog.ui.common

import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import ro.mbyte.sofalog.ui.details.parts.DetailIcons

// The brand's own colour: this mark is not themed, so it reads as the logo it is.
private val GitHubBlack = Color(0xFF181717)

/**
 * GitHub's mark, for the 40dp slot the settings cards give their icon. GitHub publishes it in two
 * colours only, white for dark backgrounds and black for light ones. The mark is itself a disc, so
 * it fills the slot with no plate behind.
 */
@Composable
internal fun GitHubMark(size: Dp = 40.dp) {
    val dark = MaterialTheme.colorScheme.surface.luminance() < 0.5f
    Icon(
        DetailIcons.GitHub,
        contentDescription = null,
        modifier = Modifier.size(size),
        tint = if (dark) Color.White else GitHubBlack,
    )
}
