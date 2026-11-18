package ro.mbyte.sofalog.ui.common

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TimePicker
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.rememberTimePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import ro.mbyte.sofalog.domain.releaseInstant
import ro.mbyte.sofalog.domain.wallTimeInstant
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit

private enum class WatchedAtMode(val label: String) {
    NOW("Now"),
    RELEASE("Release date"),
    CUSTOM("Custom date"),
}

private val timeFormat = DateTimeFormatter.ofPattern("HH:mm")

/**
 * The form behind "Mark as watched": when the viewing happened — now, on the release date
 * (midnight that day), or a date and time the user picks — and an optional rating. [onConfirm] gets a
 * null instant for "now" (the write reads its own clock) and a null rating for "leave it alone".
 * A future instant is never offered: the release option is off for an unaired or undated title
 * and a future custom time disables the confirm button.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WatchLogForm(
    title: String,
    subtitle: String,
    airDate: String?,
    onConfirm: (watchedAt: Instant?, rating: Int?) -> Unit,
    modifier: Modifier = Modifier,
) {
    val zone = remember { ZoneId.systemDefault() }
    val opened = remember { Instant.now() }
    val today = remember { LocalDate.ofInstant(opened, zone) }
    val release = remember(airDate) { releaseInstant(airDate, zone)?.takeIf { !it.isAfter(opened) } }

    var rating by remember { mutableStateOf<Int?>(null) }
    var mode by remember { mutableStateOf(WatchedAtMode.NOW) }
    var date by remember { mutableStateOf(today) }
    var time by remember { mutableStateOf(LocalTime.ofInstant(opened, zone).truncatedTo(ChronoUnit.MINUTES)) }
    var pickingDate by remember { mutableStateOf(false) }
    var pickingTime by remember { mutableStateOf(false) }

    val custom = wallTimeInstant(date, time, zone)
    val customInFuture = mode == WatchedAtMode.CUSTOM && custom.isAfter(Instant.now())

    Column(
        modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(title, style = MaterialTheme.typography.titleLarge, textAlign = TextAlign.Center)
        Text(subtitle, style = MaterialTheme.typography.titleSmall, textAlign = TextAlign.Center)

        SectionLabel("Watched at")
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            WatchedAtMode.entries.forEachIndexed { index, option ->
                SegmentedButton(
                    selected = mode == option,
                    onClick = { mode = option },
                    shape = SegmentedButtonDefaults.itemShape(index, WatchedAtMode.entries.size),
                    enabled = option != WatchedAtMode.RELEASE || release != null,
                    icon = {},
                ) { Text(option.label, maxLines = 1) }
            }
        }
        when (mode) {
            WatchedAtMode.NOW -> Hint("The watch is logged at the current time.")
            WatchedAtMode.RELEASE -> Hint("The watch is logged on ${formatDay(airDate).orEmpty()}, at 00:00.")
            WatchedAtMode.CUSTOM -> {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = { pickingDate = true }, modifier = Modifier.weight(1f)) {
                        Text(formatDay(date.toString()).orEmpty())
                    }
                    OutlinedButton(onClick = { pickingTime = true }, modifier = Modifier.weight(1f)) {
                        Text(time.format(timeFormat))
                    }
                }
                if (customInFuture) {
                    Text(
                        "A watch can't be in the future.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }
        }
        if (release == null && mode != WatchedAtMode.RELEASE) {
            Hint(if (airDate.isNullOrBlank()) "This episode has no release date yet." else "This episode hasn't been released yet.")
        }

        SectionLabel("Your rating (optional)")
        RatingBar(rating, onChange = { rating = it })

        Button(
            onClick = {
                val at = when (mode) {
                    WatchedAtMode.NOW -> null
                    WatchedAtMode.RELEASE -> release
                    WatchedAtMode.CUSTOM -> custom
                }
                onConfirm(at, rating)
            },
            enabled = !customInFuture,
            modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
        ) { Text("Mark as watched") }
    }

    if (pickingDate) {
        // The picker speaks UTC-midnight millis; a plain date goes in and comes out, no zone involved.
        val state = rememberDatePickerState(
            initialSelectedDateMillis = date.atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli(),
            selectableDates = object : SelectableDates {
                override fun isSelectableDate(utcTimeMillis: Long): Boolean =
                    !LocalDate.ofInstant(Instant.ofEpochMilli(utcTimeMillis), ZoneOffset.UTC).isAfter(today)

                override fun isSelectableYear(year: Int): Boolean = year <= today.year
            },
        )
        DatePickerDialog(
            onDismissRequest = { pickingDate = false },
            confirmButton = {
                TextButton(onClick = {
                    state.selectedDateMillis?.let {
                        date = LocalDate.ofInstant(Instant.ofEpochMilli(it), ZoneOffset.UTC)
                    }
                    pickingDate = false
                }) { Text("OK") }
            },
            dismissButton = { TextButton(onClick = { pickingDate = false }) { Text("Cancel") } },
        ) { DatePicker(state) }
    }

    if (pickingTime) {
        val state = rememberTimePickerState(initialHour = time.hour, initialMinute = time.minute, is24Hour = true)
        AlertDialog(
            onDismissRequest = { pickingTime = false },
            text = { TimePicker(state) },
            confirmButton = {
                TextButton(onClick = {
                    time = LocalTime.of(state.hour, state.minute)
                    pickingTime = false
                }) { Text("OK") }
            },
            dismissButton = { TextButton(onClick = { pickingTime = false }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun SectionLabel(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.fillMaxWidth().padding(top = 8.dp),
    )
}

@Composable
private fun Hint(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.fillMaxWidth(),
    )
}
