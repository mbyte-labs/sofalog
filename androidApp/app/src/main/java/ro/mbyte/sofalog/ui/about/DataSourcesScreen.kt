package ro.mbyte.sofalog.ui.about

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * The single home of every provider disclosure in the app (SPEC D-040), mirroring the web app's
 * Settings → Data Sources. No other screen repeats a provider notice.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DataSourcesScreen(onBack: () -> Unit) {
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Data sources") },
                navigationIcon = { TextButton(onClick = onBack) { Text("Back") } },
            )
        },
    ) { padding ->
        Column(
            Modifier.fillMaxSize().padding(padding).padding(16.dp).verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text("TMDB", style = MaterialTheme.typography.titleLarge)
            Text("This product uses the TMDB API but is not endorsed or certified by TMDB.")
            Text(
                "Search results, titles, descriptions, air dates and artwork come from The Movie " +
                    "Database. This device contacts TMDB directly when you search, when you open a " +
                    "title you have not tracked yet, and when a tracked show is refreshed. Artwork " +
                    "is loaded from TMDB's image servers.",
            )
            Text("Your data", style = MaterialTheme.typography.titleLarge)
            Text(
                "What you watch is stored on this device only. Nothing about your library is " +
                    "sent to TMDB or to anyone else.",
            )
        }
    }
}
