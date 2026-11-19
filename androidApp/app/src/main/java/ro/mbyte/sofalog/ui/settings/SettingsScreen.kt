package ro.mbyte.sofalog.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import ro.mbyte.sofalog.data.tmdbaccount.TmdbLinkStatus
import ro.mbyte.sofalog.ui.common.ActionCard
import ro.mbyte.sofalog.ui.common.tmdbAccount
import ro.mbyte.sofalog.ui.details.DetailScaffold
import ro.mbyte.sofalog.ui.details.parts.DetailIcons

/**
 * The frame every settings page shares: the menu bar over one scrolling column, kept to a readable
 * width and centred so a tablet does not stretch the rows edge to edge.
 */
@Composable
fun SettingsPage(
    title: String,
    onMenu: () -> Unit,
    message: String? = null,
    onMessageShown: () -> Unit = {},
    content: @Composable ColumnScope.() -> Unit,
) {
    DetailScaffold(title, onMenu, message, onMessageShown) { modifier ->
        Box(modifier.verticalScroll(rememberScrollState()), contentAlignment = Alignment.TopCenter) {
            Column(
                Modifier.widthIn(max = 640.dp).fillMaxWidth().padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
                content = content,
            )
        }
    }
}

/** A paragraph of explanation, in the dim tone the settings pages use for it. */
@Composable
fun SettingsNote(text: String, modifier: Modifier = Modifier) {
    Text(text, modifier, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** The heading over one section of a settings page that holds more than one. */
@Composable
fun SettingsSectionTitle(text: String, modifier: Modifier = Modifier) {
    Text(text, modifier.padding(top = 8.dp), style = MaterialTheme.typography.titleMedium)
}

/**
 * Settings: the web app's sections, as far as they exist on a device. What needs a sofalog server
 * is listed and disabled until the app can be linked to one; Jellyfin is a server-side scan and
 * has no place here.
 */
@Composable
fun SettingsScreen(
    onMenu: () -> Unit,
    onAbout: () -> Unit,
    onRestore: () -> Unit,
    onDelete: () -> Unit,
    onTmdb: () -> Unit,
    onAccount: () -> Unit,
) {
    val status by tmdbAccount().status.collectAsStateWithLifecycle(TmdbLinkStatus())
    SettingsPage("Settings", onMenu) {
        ActionCard(
            DetailIcons.Box,
            "Backup & Restore",
            "Restore your library from a Ticket Box or sofalog backup, or create a backup of what is on this device.",
            onRestore,
        )
        ActionCard(
            DetailIcons.Close,
            "Delete data",
            "Remove your movies and shows, or everything sofalog keeps on this device.",
            onDelete,
        )
        ActionCard(
            DetailIcons.Star,
            "TMDB account",
            "Link your own TMDB account: import what it holds once, and keep your ratings, watchlist and favorites in sync.",
            onTmdb,
            badge = when {
                status.broken -> "Link again needed"
                status.linked -> "Linked as @${status.account?.tmdbUsername}"
                else -> "Not linked"
            },
        )
        ActionCard(
            DetailIcons.Person,
            "Account",
            "Your time zone and the country for where-to-watch listings.",
            onAccount,
        )
        ActionCard(
            DetailIcons.Person,
            "Users",
            "Add accounts, reset passwords, remove people.",
            onClick = {},
            enabled = false,
            badge = "Needs a linked server",
        )
        ActionCard(
            DetailIcons.Close,
            "Log out",
            "Remove this app's link to a sofalog server. What is on this device stays.",
            onClick = {},
            enabled = false,
            badge = "Not linked to a server",
        )
        ActionCard(
            DetailIcons.Globe,
            "About",
            "The story behind sofalog, where your data lives, and where to find the community.",
            onAbout,
        )
    }
}
