package ro.mbyte.sofalog.ui.common

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import ro.mbyte.sofalog.R

/**
 * The app's mark. The launcher icon is an adaptive icon, which a painter cannot load, so its two
 * layers are stacked here and clipped the way a launcher would mask them.
 */
@Composable
internal fun AppLogo(size: Dp = 96.dp, corner: Dp = 22.dp) {
    Box(Modifier.size(size).clip(RoundedCornerShape(corner))) {
        Image(painterResource(R.drawable.ic_launcher_background), contentDescription = null, modifier = Modifier.fillMaxSize())
        Image(painterResource(R.drawable.ic_launcher_foreground), contentDescription = "Sofalog", modifier = Modifier.fillMaxSize())
    }
}
