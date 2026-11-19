package ro.mbyte.sofalog.ui.common

import androidx.compose.foundation.layout.Arrangement
import ro.mbyte.sofalog.domain.UserZone
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import ro.mbyte.sofalog.domain.BulkWatchMode
import ro.mbyte.sofalog.domain.wallTimeInstant
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.temporal.ChronoUnit

private enum class BulkWhen(val label: String) {
    NOW("Now"),
    FIXED("A specific date & time"),
    EPISODE_AIR("Each episode's release date"),
    SEASON_AIR("Each season's premiere date"),
}

/**
 * The form behind a show's "Mark As Watched": which episodes — the ones left, or all of them again
 * — and when they were watched. It asks two independent things, which is why [onConfirm] gets two
 * values. A bulk mark carries no rating: the page has its own bar.
 *
 * [watched] and [aired] are the show's derived progress; the "which episodes" question only exists
 * once something is watched. [seasonsNeedingDate] counts the seasons the premiere mode cannot
 * date — undated, or not yet premiered — whose episodes take a date the user picks instead.
 */
@Composable
fun BulkWatchForm(
    title: String,
    subtitle: String,
    watched: Int,
    aired: Int,
    seasonsNeedingDate: Int,
    onConfirm: (rewatch: Boolean, mode: BulkWatchMode) -> Unit,
    modifier: Modifier = Modifier,
    note: String? = null,
    enabled: Boolean = true,
) {
    val zone = remember { UserZone.current }
    val opened = remember { Instant.now() }
    val today = remember { LocalDate.ofInstant(opened, zone) }
    val remaining = (aired - watched).coerceAtLeast(0)

    // A finished show can only be watched again; anything else defaults to what is left.
    var rewatch by remember { mutableStateOf(watched > 0 && remaining == 0) }
    var mode by remember { mutableStateOf(BulkWhen.NOW) }
    var date by remember { mutableStateOf(today) }
    var time by remember { mutableStateOf(LocalTime.ofInstant(opened, zone).truncatedTo(ChronoUnit.MINUTES)) }

    val fallbackNeeded = mode == BulkWhen.SEASON_AIR && seasonsNeedingDate > 0
    val picking = mode == BulkWhen.FIXED || fallbackNeeded
    val picked = wallTimeInstant(date, time, zone)
    val pickedInFuture = picking && picked.isAfter(Instant.now())

    Column(
        modifier.fillMaxWidth().verticalScroll(rememberScrollState()),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(title, style = MaterialTheme.typography.titleLarge, textAlign = TextAlign.Center)
        Text(subtitle, style = MaterialTheme.typography.titleSmall, textAlign = TextAlign.Center)

        if (watched > 0) {
            FormLabel("Which episodes")
            Column(Modifier.fillMaxWidth().selectableGroup()) {
                Choice(
                    "Mark the $remaining remaining ${if (remaining == 1) "episode" else "episodes"}",
                    selected = !rewatch,
                    enabled = remaining > 0,
                ) { rewatch = false }
                Choice(
                    "Log all $aired ${if (aired == 1) "episode" else "episodes"} again — a full rewatch",
                    selected = rewatch,
                ) { rewatch = true }
            }
        }

        FormLabel("Watched when")
        Column(Modifier.fillMaxWidth().selectableGroup()) {
            BulkWhen.entries.forEach { option ->
                Choice(option.label, selected = mode == option) { mode = option }
            }
        }
        if (picking) {
            DateTimeFields(date, time, today, onDate = { date = it }, onTime = { time = it })
            if (fallbackNeeded) {
                FormHint(
                    if (seasonsNeedingDate == 1) {
                        "1 season has no premiere date — its episodes get this date instead."
                    } else {
                        "$seasonsNeedingDate seasons have no premiere date — their episodes get this date instead."
                    },
                )
            }
            if (pickedInFuture) {
                Text(
                    "A watch can't be in the future.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
        note?.let { FormHint(it) }

        Button(
            onClick = {
                onConfirm(
                    rewatch,
                    when (mode) {
                        BulkWhen.NOW -> BulkWatchMode.Now
                        BulkWhen.FIXED -> BulkWatchMode.Fixed(picked)
                        BulkWhen.EPISODE_AIR -> BulkWatchMode.EpisodeAir
                        BulkWhen.SEASON_AIR -> BulkWatchMode.SeasonAir(picked.takeIf { fallbackNeeded })
                    },
                )
            },
            enabled = enabled && !pickedInFuture,
            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
        ) { Text("Mark as watched") }
    }
}

/** One radio row, the whole row its touch target. */
@Composable
private fun Choice(label: String, selected: Boolean, enabled: Boolean = true, onSelect: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .selectable(selected = selected, enabled = enabled, role = Role.RadioButton, onClick = onSelect)
            .heightIn(min = 48.dp)
            .alpha(if (enabled) 1f else 0.45f),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        RadioButton(selected = selected, onClick = null, enabled = enabled)
        Text(label, style = MaterialTheme.typography.bodyLarge)
    }
}
