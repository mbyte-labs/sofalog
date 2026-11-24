package ro.mbyte.sofalog.ui.settings

import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.data.tmdbaccount.SetupResult
import ro.mbyte.sofalog.data.tmdbaccount.TmdbAccountService
import ro.mbyte.sofalog.data.tmdbaccount.TmdbLinkStatus
import ro.mbyte.sofalog.domain.KindSetup
import ro.mbyte.sofalog.domain.PULL_FAILED
import ro.mbyte.sofalog.domain.PULL_RUNNING
import ro.mbyte.sofalog.domain.SetupDraft
import ro.mbyte.sofalog.domain.SyncAction
import ro.mbyte.sofalog.domain.SyncKinds
import ro.mbyte.sofalog.domain.TmdbPullJob
import ro.mbyte.sofalog.domain.pendingLine
import ro.mbyte.sofalog.domain.pullErrorMessage
import ro.mbyte.sofalog.domain.pullProgressLine
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.formatInstant
import ro.mbyte.sofalog.ui.common.tmdbAccount
import ro.mbyte.sofalog.ui.common.userMessage
import ro.mbyte.sofalog.ui.details.parts.ChoiceSheet
import ro.mbyte.sofalog.ui.details.parts.SheetAction

class TmdbAccountViewModel(private val service: TmdbAccountService) : ViewModel() {
    val status = service.status.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)
    val message = service.message
    val draft = MutableStateFlow(SetupDraft())
    val busy = MutableStateFlow(false)

    /** The approval page to open in a browser; consumed by the screen. */
    val open = MutableStateFlow<String?>(null)

    private fun run(block: suspend () -> Unit) {
        if (busy.value) return
        busy.value = true
        viewModelScope.launch {
            attempt { block() }.onFailure { message.value = it.userMessage() }
            busy.value = false
        }
    }

    fun link() = run { open.value = service.startLink() }

    fun reopen() {
        open.value = service.approvalUrl()
    }

    fun noBrowser() {
        service.cancelLink()
        message.value = "No browser is installed to open TMDB."
    }

    fun cancelLink() = service.cancelLink()

    /** [manual] false is the page coming back to the front: quiet unless the link completes. */
    fun finish(manual: Boolean) {
        viewModelScope.launch { attempt { service.finishLink(manual) } }
    }

    fun unlink() = run { service.unlink() }

    fun pick(change: (SetupDraft) -> SetupDraft) = draft.update(change)

    fun confirm() = run {
        when (service.setup(draft.value)) {
            SetupResult.OK -> draft.value = SetupDraft()
            SetupResult.PULL_RUNNING -> message.value = "An import is already running."
            SetupResult.BROKEN -> message.value = "TMDB no longer accepts this link. Link again first."
            SetupResult.ALREADY_DONE, SetupResult.NOT_LINKED -> Unit
        }
    }

    fun setKinds(kinds: SyncKinds) = run { service.setKinds(kinds) }

    fun kick() = service.kick()
}

private data class KindCopy(val label: String, val pull: String, val push: String)

private val RATINGS = KindCopy(
    "Ratings",
    "Import the ratings on your TMDB account once. A rating you already have here is replaced by TMDB’s.",
    "Send the ratings you already have here to TMDB once.",
)
private val WATCHLIST = KindCopy(
    "Watchlist",
    "Add everything on your TMDB watchlist to your watchlist here, once. Nothing here is removed.",
    "Send your watchlist here to TMDB once.",
)
private val FAVORITES = KindCopy(
    "Favorites",
    "Add your TMDB favorites to your favorites here, once. Nothing here is removed.",
    "Send your favorites here to TMDB once.",
)

/**
 * The user's own TMDB account (D-029, D-036): link it, answer once what to do with what each side
 * already holds, then choose which kinds are kept in sync — one way, from here to TMDB.
 */
@Composable
fun TmdbAccountScreen(onMenu: () -> Unit, onAbout: () -> Unit) {
    val service = tmdbAccount()
    val vm: TmdbAccountViewModel = viewModel(factory = viewModelFactory { initializer { TmdbAccountViewModel(service) } })
    val status by vm.status.collectAsStateWithLifecycle()
    val message by vm.message.collectAsStateWithLifecycle()
    val draft by vm.draft.collectAsStateWithLifecycle()
    val busy by vm.busy.collectAsStateWithLifecycle()
    val open by vm.open.collectAsStateWithLifecycle()
    var confirmUnlink by remember { mutableStateOf(false) }
    val context = LocalContext.current

    // Coming back from the browser: try to finish a pending link without asking for a tap, and
    // send whatever is queued.
    LifecycleResumeEffect(Unit) {
        vm.finish(manual = false)
        vm.kick()
        onPauseOrDispose {}
    }
    LaunchedEffect(open) {
        val url = open ?: return@LaunchedEffect
        vm.open.value = null
        try {
            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
        } catch (e: ActivityNotFoundException) {
            vm.noBrowser()
        }
    }

    SettingsPage("TMDB account", onMenu, message, { vm.message.value = null }) {
        SettingsNote(
            "Link your own TMDB account, choose once what to do with the ratings, watchlist and favorites " +
                "each side already holds, and from then on the kinds you keep in sync are sent to it — one " +
                "way. After that one-time setup nothing is read back, and removing something here removes " +
                "it there. Unlinking stops the sending and leaves your TMDB account as it is.",
        )
        TextButton(onClick = onAbout) { Text("About TMDB and your data") }

        val s = status ?: return@SettingsPage
        val pull = s.pull
        when {
            !s.configured -> SettingsNote("This build has no TMDB key, so accounts cannot be linked.")
            s.awaitingApproval -> AwaitingCard(busy, { vm.finish(manual = true) }, vm::reopen, vm::cancelLink)
            !s.linked -> Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text("No TMDB account is linked.", style = MaterialTheme.typography.titleMedium)
                    SettingsNote(
                        "You will be sent to TMDB to approve the link, then brought back here to choose, for " +
                            "your ratings, watchlist and favorites, whether to import TMDB’s list, send yours, " +
                            "or do nothing — and which to keep in sync from then on.",
                    )
                    Button(onClick = vm::link, enabled = !busy, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) {
                        Text("Link TMDB account")
                    }
                }
            }
            else -> {
                val running = pull?.state == PULL_RUNNING
                if (pull?.state == PULL_FAILED) {
                    Text(pullErrorMessage(pull.error.orEmpty()), color = MaterialTheme.colorScheme.error)
                } else if (s.pullInterrupted) {
                    Text(pullErrorMessage("cancelled"), color = MaterialTheme.colorScheme.error)
                }
                if (!s.account!!.setupDone && !s.broken && !running) SetupCard(draft, busy, vm::pick, vm::confirm)
                if (pull != null) PullCard(pull)
                LinkCard(s, busy, vm::link) { confirmUnlink = true }
                if (s.account.setupDone) KeepInSyncCard(s, busy, vm::setKinds)
            }
        }
    }

    if (confirmUnlink) {
        ChoiceSheet(
            title = "Unlink TMDB account?",
            subject = "@${status?.account?.tmdbUsername.orEmpty()}",
            note = "Stop sending ratings, watchlist and favorite changes. Your TMDB account stays as it is.",
            actions = listOf(
                SheetAction("Yes, unlink", danger = true) {
                    confirmUnlink = false
                    vm.unlink()
                },
            ),
            onDismiss = { confirmUnlink = false },
        )
    }
}

@Composable
private fun AwaitingCard(busy: Boolean, onFinish: () -> Unit, onReopen: () -> Unit, onCancel: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Approve the link in your browser, then come back.", style = MaterialTheme.typography.titleMedium)
            SettingsNote("If TMDB does not bring you back here by itself, return to sofalog and finish the link below.")
            Button(onClick = onFinish, enabled = !busy, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) {
                Text("I approved it — finish linking")
            }
            OutlinedButton(onClick = onReopen, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) { Text("Open TMDB again") }
            TextButton(onClick = onCancel, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) { Text("Cancel") }
        }
    }
}

@Composable
private fun SetupCard(draft: SetupDraft, busy: Boolean, pick: ((SetupDraft) -> SetupDraft) -> Unit, onConfirm: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text("Set up the sync", style = MaterialTheme.typography.titleMedium)
            SettingsNote(
                "For each kind, choose what happens now with what you and TMDB already hold. Nothing is sent " +
                    "or imported until you confirm.",
            )
            KindSetupRow(RATINGS, draft.ratings) { next -> pick { it.copy(ratings = next) } }
            HorizontalDivider()
            KindSetupRow(WATCHLIST, draft.watchlist) { next -> pick { it.copy(watchlist = next) } }
            HorizontalDivider()
            KindSetupRow(FAVORITES, draft.favorites) { next -> pick { it.copy(favorites = next) } }
            Button(onClick = onConfirm, enabled = !busy, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) { Text("Confirm") }
        }
    }
}

@Composable
private fun KindSetupRow(copy: KindCopy, value: KindSetup, onChange: (KindSetup) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(copy.label, style = MaterialTheme.typography.titleSmall)
        val actions = listOf(SyncAction.PULL to "Pull", SyncAction.PUSH to "Push", SyncAction.SKIP to "Skip")
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            actions.forEachIndexed { index, (action, label) ->
                SegmentedButton(
                    selected = value.action == action,
                    onClick = { onChange(value.pick(action)) },
                    shape = SegmentedButtonDefaults.itemShape(index, actions.size),
                    modifier = Modifier.heightIn(min = 48.dp),
                ) { Text(label) }
            }
        }
        SettingsNote(
            when (value.action) {
                SyncAction.PULL -> copy.pull
                SyncAction.PUSH -> copy.push
                SyncAction.SKIP -> "Do nothing now."
            },
        )
        SwitchRow("Keep ${copy.label.lowercase()} in sync from now on", value.sync, true) { onChange(value.copy(sync = it)) }
    }
}

/** A switch whose whole row is the tap target. */
@Composable
private fun SwitchRow(label: String, checked: Boolean, enabled: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .toggleable(value = checked, enabled = enabled, role = Role.Switch, onValueChange = onChange)
            .heightIn(min = 56.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(label, Modifier.weight(1f))
        Switch(checked = checked, onCheckedChange = null, enabled = enabled)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun PullCard(pull: TmdbPullJob) {
    val running = pull.state == PULL_RUNNING
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(if (running) "Importing from TMDB" else "Last import from TMDB", style = MaterialTheme.typography.titleMedium)
            if (running) {
                Text(pullProgressLine(pull))
                LinearProgressIndicator(Modifier.fillMaxWidth())
                SettingsNote("Keep sofalog open until the import finishes.")
            }
            val r = pull.report
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Counter("Ratings imported", r.ratings)
                Counter("Episode ratings imported", r.episodeRatings)
                Counter("Added to watchlist", r.watchlist)
                Counter("Added to favorites", r.favorites)
                Counter("Movies added to the library", r.moviesAdded)
                Counter("Shows added to the library", r.showsAdded)
                Counter("Skipped", r.skipped)
            }
        }
    }
}

/** One number of a report, as a tile. */
@Composable
internal fun Counter(label: String, value: Int, modifier: Modifier = Modifier) {
    Card(modifier.widthIn(min = 140.dp)) {
        Column(Modifier.padding(12.dp)) {
            Text(value.toString(), style = MaterialTheme.typography.headlineSmall)
            Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun LinkCard(s: TmdbLinkStatus, busy: Boolean, onRelink: () -> Unit, onUnlink: () -> Unit) {
    val account = s.account ?: return
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("Linked as @${account.tmdbUsername}", style = MaterialTheme.typography.titleMedium)
            SettingsNote("Since ${formatInstant(account.linkedAt)}")
            if (s.broken) {
                Text(
                    "TMDB no longer accepts this link. Nothing is being sent — link again to resume" +
                        (if (account.setupDone) "." else ", then finish the setup."),
                    color = MaterialTheme.colorScheme.error,
                )
                Button(onClick = onRelink, enabled = !busy, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) { Text("Link again") }
            } else {
                Text(pendingLine(s.pending))
                if (s.pending > 0) SettingsNote("Changes are sent while sofalog is open.")
            }
            OutlinedButton(onClick = onUnlink, enabled = !busy, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) { Text("Unlink") }
        }
    }
}

@Composable
private fun KeepInSyncCard(s: TmdbLinkStatus, busy: Boolean, onChange: (SyncKinds) -> Unit) {
    val account = s.account ?: return
    val kinds = SyncKinds(account.syncRatings, account.syncWatchlist, account.syncFavorites)
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("Keep in sync", style = MaterialTheme.typography.titleMedium)
            SettingsNote(
                "From now on, changes you make here are sent to TMDB for the kinds you turn on. Turning one " +
                    "off stops the sending and forgets what was still queued; turning it on does not resend " +
                    "what you already have.",
            )
            SwitchRow("Ratings", kinds.ratings, !busy) { onChange(kinds.copy(ratings = it)) }
            SwitchRow("Watchlist", kinds.watchlist, !busy) { onChange(kinds.copy(watchlist = it)) }
            SwitchRow("Favorites", kinds.favorites, !busy) { onChange(kinds.copy(favorites = it)) }
        }
    }
}
