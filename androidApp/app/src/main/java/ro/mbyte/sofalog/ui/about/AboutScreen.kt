package ro.mbyte.sofalog.ui.about

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import ro.mbyte.sofalog.BuildConfig
import ro.mbyte.sofalog.R
import ro.mbyte.sofalog.ui.common.ActionCard
import ro.mbyte.sofalog.ui.common.AppLogo
import ro.mbyte.sofalog.ui.common.GitHubMark
import ro.mbyte.sofalog.ui.details.parts.DetailIcons
import ro.mbyte.sofalog.ui.details.parts.open
import ro.mbyte.sofalog.ui.settings.SettingsNote
import ro.mbyte.sofalog.ui.settings.SettingsPage

// Placeholder copy and destinations: the story is to be rewritten, and the Discord and Terms
// links do not point at their real pages yet.
private const val STORY =
    "Sofalog is a passion project. I build it in my free time, because I wanted a simple, private " +
        "place to keep track of what I watch — and because I enjoy making it. There are no ads and " +
        "no tracking, and there is no company behind it: just one person and the evenings left " +
        "over after work."
private const val GITHUB_URL = "https://github.com/mbyte-labs/sofalog"
private const val DISCORD_URL = "https://google.com"
private const val TERMS_URL = "https://sofalog.mbyte.ro/"

// The brand's own colour: this mark is not themed, so it reads as the logo it is.
private val DiscordBlurple = Color(0xFF5865F2)

/**
 * Settings → About: the app's identity, its story, where the user's data lives and where to find
 * the community. It is also the single home of every provider disclosure in the app (SPEC D-040),
 * the counterpart of the web app's Settings → Data Sources. No other screen repeats a provider
 * notice.
 */
@Composable
fun AboutScreen(onMenu: () -> Unit) {
    val uri = LocalUriHandler.current
    SettingsPage("About", onMenu) {
        Column(
            Modifier.fillMaxWidth().padding(vertical = 12.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            AppLogo()
            Text("Sofalog", Modifier.padding(top = 8.dp), style = MaterialTheme.typography.headlineSmall)
            Text(
                "Version ${BuildConfig.VERSION_NAME}",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }

        AboutSection("The story") {
            Card(Modifier.fillMaxWidth()) {
                Text(STORY, Modifier.padding(16.dp), style = MaterialTheme.typography.bodyMedium)
            }
        }

        AboutSection("Your data") {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                    DataRow(
                        "On this device",
                        "Always. Everything you track — your watch history, ratings, watchlist, " +
                            "favorites and copies — is stored in the app's own database on this device.",
                    ) {
                        Icon(DetailIcons.Phone, null, Modifier.size(28.dp), tint = MaterialTheme.colorScheme.primary)
                    }
                    DataRow(
                        "On your sofalog server",
                        "Only if you connect this app to a sofalog server. Then your library is " +
                            "kept there too, and the server's copy is the one that counts.",
                    ) {
                        Icon(DetailIcons.Server, null, Modifier.size(28.dp), tint = MaterialTheme.colorScheme.primary)
                    }
                    DataRow(
                        "On TMDB",
                        "Only if you link your own TMDB account under Settings. Then the ratings, " +
                            "watchlist and favorites you chose to keep in sync are sent to that " +
                            "account, and its lists are read once if you ask for them to be " +
                            "imported. Your watch history is never sent to anyone.",
                    ) {
                        // TMDB's official square logo, the form meant for small sizes: never tinted.
                        Image(painterResource(R.drawable.tmdb_logo_square), contentDescription = "TMDB", modifier = Modifier.width(40.dp))
                    }
                    HorizontalDivider()
                    Image(
                        painterResource(R.drawable.tmdb_logo),
                        contentDescription = "TMDB",
                        modifier = Modifier.height(14.dp),
                    )
                    Text(
                        "This product uses the TMDB API but is not endorsed or certified by TMDB.",
                        style = MaterialTheme.typography.bodyMedium,
                    )
                    SettingsNote(
                        "Search results, titles, descriptions, air dates and artwork come from The " +
                            "Movie Database. This device contacts TMDB directly when you search, when " +
                            "you open a title you have not tracked yet, and when a tracked show is " +
                            "refreshed. Artwork is loaded from TMDB's image servers.",
                    )
                    SettingsNote(
                        "Restoring a Ticket Box backup reads the file you pick and nothing else; the " +
                            "titles it names are fetched from TMDB like any other.",
                    )
                }
            }
        }

        AboutSection("Community") {
            ActionCard(
                "GitHub",
                "The source code, the issue tracker and the releases.",
                onClick = { uri.open(GITHUB_URL) },
            ) {
                GitHubMark()
            }
            ActionCard(
                "Discord",
                "Ask a question, share an idea, or just say hi.",
                onClick = { uri.open(DISCORD_URL) },
            ) {
                BrandDisc(DetailIcons.Discord, background = DiscordBlurple, tint = Color.White, markSize = 24.dp)
            }
            ActionCard(
                "Terms of Service",
                "The terms the app is offered under.",
                onClick = { uri.open(TERMS_URL) },
            ) {
                AppLogo(size = 40.dp, corner = 10.dp)
            }
        }
    }
}

/** A brand's mark in its own colours, in the 40dp slot the settings cards give their icon. */
@Composable
private fun BrandDisc(mark: ImageVector, background: Color, tint: Color, markSize: Dp) {
    Box(Modifier.size(40.dp).background(background, CircleShape), contentAlignment = Alignment.Center) {
        Icon(mark, contentDescription = null, modifier = Modifier.size(markSize), tint = tint)
    }
}

/** An uppercase label over the section's content. */
@Composable
private fun AboutSection(title: String, content: @Composable ColumnScope.() -> Unit) {
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(
            title.uppercase(),
            Modifier.padding(horizontal = 4.dp),
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.primary,
        )
        content()
    }
}

/** One place the data lives: its mark in a fixed-width slot, then the name and the condition. */
@Composable
private fun DataRow(title: String, text: String, mark: @Composable () -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        Box(Modifier.width(40.dp).padding(top = 2.dp), contentAlignment = Alignment.Center) { mark() }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(title, style = MaterialTheme.typography.titleSmall)
            SettingsNote(text)
        }
    }
}
