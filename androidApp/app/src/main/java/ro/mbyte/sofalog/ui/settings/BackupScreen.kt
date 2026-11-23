package ro.mbyte.sofalog.ui.settings

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.data.BackupRepository
import ro.mbyte.sofalog.data.RestoreRunningException
import ro.mbyte.sofalog.data.RestoreState
import ro.mbyte.sofalog.data.SharedBackup
import ro.mbyte.sofalog.domain.RESTORE_RUNNING
import ro.mbyte.sofalog.domain.backupSummaryLine
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.backupRepository
import ro.mbyte.sofalog.ui.common.restoreRepository

private const val BACKUP_MIME = "application/json"

class BackupViewModel(private val backup: BackupRepository) : ViewModel() {
    val message = MutableStateFlow<String?>(null)
    val busy = MutableStateFlow(false)

    /** A backup written for the share sheet; consumed by the screen. */
    val share = MutableStateFlow<SharedBackup?>(null)

    fun fileName(): String = backup.fileName()

    private fun run(block: suspend () -> Unit) {
        if (busy.value) return
        busy.value = true
        viewModelScope.launch {
            attempt { block() }.onFailure {
                message.value = if (it is RestoreRunningException) {
                    "An import is running. Create the backup when it has finished."
                } else {
                    "The backup couldn't be written."
                }
            }
            busy.value = false
        }
    }

    fun save(uri: Uri) = run { message.value = "Backup saved. " + backupSummaryLine(backup.writeTo(uri)) }

    fun share() = run { share.value = backup.writeShareFile() }

    fun noApp() {
        message.value = "No app on this device can take the file."
    }
}

/**
 * Creating the app's own backup file (D-040): the same JSON either saved where the user points —
 * this device, or any storage app it offers — or handed to another app through the share sheet,
 * which is also how it reaches another device.
 */
@Composable
fun BackupScreen(onMenu: () -> Unit) {
    val backup = backupRepository()
    val restore = restoreRepository()
    val vm: BackupViewModel = viewModel(factory = viewModelFactory { initializer { BackupViewModel(backup) } })
    val message by vm.message.collectAsStateWithLifecycle()
    val busy by vm.busy.collectAsStateWithLifecycle()
    val share by vm.share.collectAsStateWithLifecycle()
    val restoreState by restore.state.collectAsStateWithLifecycle()
    val importing = (restoreState as? RestoreState.Active)?.job?.state == RESTORE_RUNNING
    val context = LocalContext.current

    val saver = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument(BACKUP_MIME)) { uri -> uri?.let(vm::save) }

    LaunchedEffect(share) {
        val shared = share ?: return@LaunchedEffect
        vm.share.value = null
        val send = Intent(Intent.ACTION_SEND).apply {
            type = BACKUP_MIME
            putExtra(Intent.EXTRA_STREAM, shared.uri)
            putExtra(Intent.EXTRA_SUBJECT, shared.fileName)
            // The clip is what carries the read grant to the app the user picks, and names the file
            // in the share sheet's preview.
            clipData = ClipData.newRawUri(shared.fileName, shared.uri)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        try {
            context.startActivity(Intent.createChooser(send, "Share backup"))
        } catch (e: ActivityNotFoundException) {
            vm.noApp()
        }
    }

    SettingsPage("Create a backup", onMenu, message, { vm.message.value = null }) {
        SettingsNote(
            "A backup is one JSON file with everything you logged: your watch history, ratings, watchlist, " +
                "favorites, dropped shows and your copies. Restore it from Backup & Restore, on this device " +
                "or another one.",
        )
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text("What is not in it", style = MaterialTheme.typography.titleSmall)
                SettingsNote(
                    "Posters and descriptions — a restore fetches them from TMDB again. Your settings and your " +
                        "TMDB account link stay on this device; after a restore elsewhere, link the account again.",
                )
            }
        }
        if (importing) SettingsNote("An import is running. A backup can be created when it has finished.")
        Button(
            onClick = { saver.launch(vm.fileName()) },
            enabled = !busy && !importing,
            modifier = Modifier.fillMaxWidth().height(56.dp),
        ) {
            if (busy) {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                    CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                    Text("Writing the backup…")
                }
            } else {
                Text("Save to device")
            }
        }
        OutlinedButton(
            onClick = vm::share,
            enabled = !busy && !importing,
            modifier = Modifier.fillMaxWidth().height(56.dp),
        ) { Text("Share…") }
        SettingsNote(
            "Share sends the file to another app — a cloud drive, mail, or a nearby device. The file is not " +
                "encrypted: anyone who has it can read what you watched.",
        )
    }
}
