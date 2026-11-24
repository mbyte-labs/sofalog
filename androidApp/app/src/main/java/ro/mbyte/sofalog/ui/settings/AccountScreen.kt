package ro.mbyte.sofalog.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.selectable
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import ro.mbyte.sofalog.data.AppSettings
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.domain.countries
import ro.mbyte.sofalog.domain.countryName
import ro.mbyte.sofalog.domain.zoneLabel
import ro.mbyte.sofalog.domain.zoneNames
import ro.mbyte.sofalog.ui.common.attempt
import ro.mbyte.sofalog.ui.common.repository
import ro.mbyte.sofalog.ui.details.DetailScaffold
import java.time.ZoneId

class AccountViewModel(private val repo: VideoRepository) : ViewModel() {
    val settings = repo.observeSettings().stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AppSettings())
    val message = MutableStateFlow<String?>(null)

    fun setTimeZone(name: String?, then: () -> Unit = {}) = save(then) { repo.setTimeZone(name) }

    fun setRegion(code: String, then: () -> Unit = {}) = save(then) { repo.setWatchRegion(code) }

    private fun save(then: () -> Unit, write: suspend () -> Unit) {
        viewModelScope.launch {
            attempt { write() }.onSuccess { then() }.onFailure { message.value = "Could not save the setting." }
        }
    }
}

@Composable
private fun accountViewModel(): AccountViewModel {
    val repo = repository()
    return viewModel(factory = viewModelFactory { initializer { AccountViewModel(repo) } })
}

/**
 * The web app's Account page, as far as it exists on a device: the time zone and the country are
 * the device's own; the name, sign-in and matching parts belong to a server account.
 */
@Composable
fun AccountScreen(onMenu: () -> Unit, onPickZone: () -> Unit, onPickRegion: () -> Unit) {
    val vm = accountViewModel()
    val settings by vm.settings.collectAsStateWithLifecycle()
    val message by vm.message.collectAsStateWithLifecycle()
    val device = remember { ZoneId.systemDefault().id }

    SettingsPage("Account", onMenu, message, { vm.message.value = null }) {
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Time zone", style = MaterialTheme.typography.titleMedium)
                SettingsNote(
                    "Where your day ends: which episodes count as aired, what the app calls \"Today\", and " +
                        "the zone every date and time is shown in. Everything is stored in UTC, so changing " +
                        "this moves nothing you have logged — only how it reads.",
                )
                Text(
                    settings.timeZone?.let(::zoneLabel) ?: "Device default (${zoneLabel(device)})",
                    style = MaterialTheme.typography.titleSmall,
                )
                OutlinedButton(onClick = onPickZone, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) {
                    Text("Change time zone")
                }
                if (settings.timeZone != null && settings.timeZone != device) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        SettingsNote("This device is set to ${zoneLabel(device)}.", Modifier.weight(1f))
                        TextButton(onClick = { vm.setTimeZone(null) }) { Text("Use it") }
                    }
                }
            }
        }
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Where to watch country", style = MaterialTheme.typography.titleMedium)
                SettingsNote(
                    "The country whose streaming, rental and purchase listings the \"Watch now\" section " +
                        "shows. After a change, each title you already track refreshes its listing the " +
                        "next time you open it.",
                )
                Text("${countryName(settings.watchRegion)} (${settings.watchRegion})", style = MaterialTheme.typography.titleSmall)
                OutlinedButton(onClick = onPickRegion, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) {
                    Text("Change country")
                }
            }
        }
        Card(Modifier.fillMaxWidth().alpha(0.6f)) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Server account", style = MaterialTheme.typography.titleMedium)
                SettingsNote(
                    "Display name, email and password, and the matching watchlist switch belong to an " +
                        "account on a sofalog server. Available once this app is linked to a server.",
                )
            }
        }
    }
}

private data class PickerOption(val value: String?, val label: String, val detail: String? = null)

@Composable
fun ZonePickerScreen(onMenu: () -> Unit, onDone: () -> Unit) {
    val vm = accountViewModel()
    val settings by vm.settings.collectAsStateWithLifecycle()
    val options = remember {
        val device = ZoneId.systemDefault().id
        listOf(PickerOption(null, "Device default", zoneLabel(device))) + zoneNames().map { PickerOption(it, zoneLabel(it)) }
    }
    Picker("Time zone", "Search time zones", options, settings.timeZone, onMenu) { vm.setTimeZone(it, onDone) }
}

@Composable
fun RegionPickerScreen(onMenu: () -> Unit, onDone: () -> Unit) {
    val vm = accountViewModel()
    val settings by vm.settings.collectAsStateWithLifecycle()
    val options = remember { countries().map { PickerOption(it.code, it.name, it.code) } }
    Picker("Country", "Search countries", options, settings.watchRegion, onMenu) { it?.let { code -> vm.setRegion(code, onDone) } }
}

/** A long single-choice list as its own page: a search field over full-width rows, one tap to pick. */
@Composable
private fun Picker(
    title: String,
    hint: String,
    options: List<PickerOption>,
    selected: String?,
    onMenu: () -> Unit,
    onPick: (String?) -> Unit,
) {
    var query by rememberSaveable { mutableStateOf("") }
    val shown = remember(query, options) {
        val q = query.trim()
        if (q.isEmpty()) options else options.filter { it.label.contains(q, ignoreCase = true) || it.detail?.contains(q, ignoreCase = true) == true }
    }
    DetailScaffold(title, onMenu, null, {}) { modifier ->
        Column(modifier) {
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                placeholder = { Text(hint) },
                singleLine = true,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
            )
            LazyColumn(Modifier.weight(1f)) {
                items(shown, key = { it.value ?: "" }) { option ->
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .selectable(selected = option.value == selected, role = Role.RadioButton) { onPick(option.value) }
                            .heightIn(min = 56.dp)
                            .padding(horizontal = 16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        RadioButton(selected = option.value == selected, onClick = null)
                        Column(Modifier.weight(1f)) {
                            Text(option.label)
                            option.detail?.let {
                                Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                    }
                }
            }
        }
    }
}
