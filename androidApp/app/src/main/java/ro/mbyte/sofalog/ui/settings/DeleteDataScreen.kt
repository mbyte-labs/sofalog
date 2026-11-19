package ro.mbyte.sofalog.ui.settings

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil3.SingletonImageLoader
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import ro.mbyte.sofalog.data.RestoreState
import ro.mbyte.sofalog.data.tmdbaccount.TmdbLinkStatus
import ro.mbyte.sofalog.domain.PULL_RUNNING
import ro.mbyte.sofalog.domain.RESTORE_RUNNING
import ro.mbyte.sofalog.ui.common.ConfirmDialog
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.common.restoreRepository
import ro.mbyte.sofalog.ui.common.tmdbAccount

/**
 * Delete data: empty the video library, or take the app back to a first launch. Both only clear
 * this device — nothing is ever deleted on TMDB — and both are refused while an import is
 * writing, since it would refill the tables behind the delete.
 */
@Composable
fun DeleteDataScreen(onMenu: () -> Unit, onDeleted: () -> Unit) {
    val repo = repository()
    val account = tmdbAccount()
    val restore = restoreRepository()
    val context = LocalContext.current.applicationContext
    val scope = rememberCoroutineScope()

    val restoreState by restore.state.collectAsStateWithLifecycle()
    val link by account.status.collectAsStateWithLifecycle(TmdbLinkStatus())
    val importing = (restoreState as? RestoreState.Active)?.job?.state == RESTORE_RUNNING ||
        link.pull?.state == PULL_RUNNING

    var busy by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    var confirmLibrary by remember { mutableStateOf(false) }
    var confirmEverything by remember { mutableStateOf(false) }
    val enabled = !busy && !importing

    SettingsPage("Delete data", onMenu, message, { message = null }) {
        if (importing) {
            SettingsNote("An import is running. Wait for it to finish, or stop it, before deleting anything.")
        }

        SettingsSectionTitle("Movies and shows")
        SettingsNote(
            "Removes your whole library from this device: watch history, ratings, watchlist, favorites, " +
                "dropped shows and owned copies, with the titles stored for them. Your settings and your " +
                "TMDB account link stay.",
        )
        OutlinedButton(
            onClick = { confirmLibrary = true },
            enabled = enabled,
            colors = ButtonDefaults.outlinedButtonColors(contentColor = MaterialTheme.colorScheme.error),
            border = BorderStroke(1.dp, MaterialTheme.colorScheme.error.copy(alpha = if (enabled) 1f else 0.38f)),
            modifier = Modifier.fillMaxWidth().height(56.dp),
        ) { Text("Delete movies and shows") }

        SettingsSectionTitle("Everything")
        SettingsNote(
            "Removes everything sofalog keeps on this device — your library, your settings, the TMDB " +
                "account link and the downloaded images — as if the app was opened for the first time.",
        )
        Button(
            onClick = { confirmEverything = true },
            enabled = enabled,
            colors = ButtonDefaults.buttonColors(
                containerColor = MaterialTheme.colorScheme.error,
                contentColor = MaterialTheme.colorScheme.onError,
            ),
            modifier = Modifier.fillMaxWidth().height(56.dp),
        ) { Text("Delete everything") }

        SettingsNote("Neither action deletes anything on TMDB: a linked account keeps its ratings and lists.")
    }

    if (confirmLibrary) {
        ConfirmDialog(
            title = "Delete movies and shows?",
            text = "Your watch history, ratings, watchlist, favorites, dropped shows and copies are removed " +
                "from this device. This cannot be undone.",
            confirm = "Delete",
            onConfirm = {
                confirmLibrary = false
                busy = true
                scope.launch {
                    val result = attempt { repo.deleteLibrary() }
                    busy = false
                    message = if (result.isSuccess) "Movies and shows deleted." else "Deleting failed. Nothing was removed."
                }
            },
            onDismiss = { confirmLibrary = false },
        )
    }
    if (confirmEverything) {
        ConfirmDialog(
            title = "Delete everything?",
            text = "Your library, your settings and the TMDB account link are removed from this device, " +
                "and sofalog starts over. This cannot be undone.",
            confirm = "Delete everything",
            onConfirm = {
                confirmEverything = false
                busy = true
                scope.launch {
                    val result = attempt {
                        restore.discard()
                        // The link's secret lives outside the database, so it is forgotten first.
                        account.unlink()
                        repo.deleteEverything()
                        val images = SingletonImageLoader.get(context)
                        images.memoryCache?.clear()
                        withContext(Dispatchers.IO) { images.diskCache?.clear() }
                    }
                    busy = false
                    if (result.isSuccess) onDeleted() else message = "Deleting failed. Try again."
                }
            },
            onDismiss = { confirmEverything = false },
        )
    }
}
