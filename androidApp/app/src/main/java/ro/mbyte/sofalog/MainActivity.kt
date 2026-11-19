package ro.mbyte.sofalog

import android.content.Intent
import android.graphics.Color
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.ui.Modifier
import ro.mbyte.sofalog.ui.SofalogNav
import ro.mbyte.sofalog.ui.theme.SofalogTheme

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // A cold start through the TMDB link's return (the app was closed while the browser was open).
        if (savedInstanceState == null) app.tmdbAccount.onLinkReturn(intent?.dataString)
        // Dark only: light system-bar icons regardless of the device's light/dark setting.
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.dark(Color.TRANSPARENT),
        )
        setContent {
            SofalogTheme {
                Surface(Modifier.fillMaxSize()) { SofalogNav() }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        app.tmdbAccount.onLinkReturn(intent.dataString)
    }

    // What is queued for the user's TMDB account is sent only while the app is on screen.
    override fun onStart() {
        super.onStart()
        app.tmdbAccount.startWorker()
    }

    override fun onStop() {
        app.tmdbAccount.stopWorker()
        super.onStop()
    }

    private val app: SofalogApp get() = application as SofalogApp
}
