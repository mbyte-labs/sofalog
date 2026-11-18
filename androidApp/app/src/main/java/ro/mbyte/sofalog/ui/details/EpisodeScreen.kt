package ro.mbyte.sofalog.ui.details

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import ro.mbyte.sofalog.ui.common.CenteredMessage
import ro.mbyte.sofalog.ui.common.episodeCode

/**
 * A placeholder: the episode page is not built yet, so this reads nothing and only names the
 * episode it was opened for. Addressed like every other page, by the show's TMDB id + position.
 */
@Composable
fun EpisodeScreen(season: Int, episode: Int, onBack: () -> Unit, onOpenShow: () -> Unit) {
    DetailScaffold(
        title = episodeCode(season, episode),
        onBack = onBack,
        message = null,
        onMessageShown = {},
    ) { modifier ->
        CenteredMessage("Episode details are coming soon.", modifier) {
            Button(onClick = onOpenShow, modifier = Modifier.padding(top = 16.dp)) { Text("Open show") }
        }
    }
}
