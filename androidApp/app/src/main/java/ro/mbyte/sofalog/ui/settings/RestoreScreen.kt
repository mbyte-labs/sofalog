package ro.mbyte.sofalog.ui.settings

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.DrawableRes
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridScope
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import ro.mbyte.sofalog.R
import ro.mbyte.sofalog.data.RestoreSource
import ro.mbyte.sofalog.data.RestoreState
import ro.mbyte.sofalog.domain.RESTORE_DONE
import ro.mbyte.sofalog.domain.RESTORE_RUNNING
import ro.mbyte.sofalog.domain.RestoreJob
import ro.mbyte.sofalog.domain.RestoreSkip
import ro.mbyte.sofalog.domain.SKIP_MOVIE_NOT_FOUND
import ro.mbyte.sofalog.domain.SKIP_SHOW_NOT_FOUND
import ro.mbyte.sofalog.domain.restorePercent
import ro.mbyte.sofalog.domain.restoreProgressLine
import ro.mbyte.sofalog.domain.skipKindLabel
import ro.mbyte.sofalog.domain.skipReasonExplanation
import ro.mbyte.sofalog.domain.skipReasonLabel
import ro.mbyte.sofalog.domain.skipTarget
import ro.mbyte.sofalog.ui.common.ActionCard
import ro.mbyte.sofalog.ui.common.ConfirmDialog
import ro.mbyte.sofalog.ui.common.GitHubMark
import ro.mbyte.sofalog.ui.common.formatInstant
import ro.mbyte.sofalog.ui.common.restoreRepository
import ro.mbyte.sofalog.ui.details.DetailScaffold
import ro.mbyte.sofalog.ui.details.parts.DetailIcons

private const val TICKET_BOX_URL = "https://github.com/mbyte-labs/ticket-box"

/**
 * Backup & Restore: which backup to restore from, and the way to create one. The app's own backup
 * file (D-040) is what makes a phone that holds the only copy of a library safe to lose.
 */
@Composable
fun RestoreScreen(onMenu: () -> Unit, onTicketBox: () -> Unit, onSofalog: () -> Unit, onBackup: () -> Unit) {
    SettingsPage("Backup & Restore", onMenu) {
        SettingsSectionTitle("Create backup")
        SettingsNote("This device holds the only copy of your library. A backup file is how you keep another.")
        ActionCard(
            DetailIcons.Box,
            "Create a backup",
            "Save everything you logged to a file on this device, or send it to another app or device.",
            onBackup,
        )

        SettingsSectionTitle("Restore backup")
        SettingsNote(
            "Bring your watch history, ratings, watchlist and favorites back from a file. Pick the app the " +
                "backup came from.",
        )
        // Each card wears its source app's own mark, as the web chooser does, in the usual icon disc.
        ActionCard(
            "Sofalog restore",
            "A backup created by this app: watch history, ratings, watchlist, favorites, dropped shows and your copies.",
            onSofalog,
        ) {
            MarkDisc(R.drawable.sofalog_mark, 34.dp)
        }
        ActionCard(
            "Ticket Box restore",
            "A backup exported from the Ticket Box app: watch history, ratings, watchlist and favorites.",
            onTicketBox,
        ) {
            MarkDisc(R.drawable.ticket_box, 28.dp)
        }
    }
}

/**
 * ActionCard's own icon disc, holding an app's mark in its own colours instead of a tinted glyph.
 * The size is per mark: each drawable carries a different amount of empty space around its shape.
 */
@Composable
private fun MarkDisc(@DrawableRes mark: Int, markSize: Dp, size: Dp = 40.dp) {
    Box(
        Modifier.size(size).background(MaterialTheme.colorScheme.primary.copy(alpha = 0.16f), CircleShape),
        contentAlignment = Alignment.Center,
    ) {
        Image(painterResource(mark), contentDescription = null, modifier = Modifier.size(markSize))
    }
}

/**
 * Restoring a Ticket Box export (D-023): pick the file, see what it holds, confirm, watch it run,
 * read the report. The job belongs to the app, not to this page — leaving and coming back finds
 * it still running, or its report.
 */
@Composable
fun TicketBoxRestoreScreen(
    onMenu: () -> Unit,
    onAbout: () -> Unit,
    onOpenMovie: (Long) -> Unit,
    onOpenShow: (Long) -> Unit,
    onOpenSeason: (Long, Int) -> Unit,
) {
    val uri = LocalUriHandler.current
    RestoreFlow(
        source = RestoreSource.TICKET_BOX,
        title = "Restore a Ticket Box dump",
        chooseLabel = "Choose a Ticket Box export…",
        confirmText = "Replace matching history with this file?",
        onMenu = onMenu,
        onOpenMovie = onOpenMovie,
        onOpenShow = onOpenShow,
        onOpenSeason = onOpenSeason,
    ) {
        wide { TicketBoxStory() }
        wide {
            ActionCard(
                "Ticket Box on GitHub",
                "The source code of the app this backup comes from.",
                onClick = { uri.openUri(TICKET_BOX_URL) },
            ) { GitHubMark() }
        }
        wide { ImportedCard() }
        wide { TmdbCard(onAbout) }
    }
}

/**
 * Restoring the app's own backup file (D-040). The file states each title's whole state, so a
 * title in it is replaced and every other title on the device is left alone.
 */
@Composable
fun SofalogRestoreScreen(
    onMenu: () -> Unit,
    onAbout: () -> Unit,
    onOpenMovie: (Long) -> Unit,
    onOpenShow: (Long) -> Unit,
    onOpenSeason: (Long, Int) -> Unit,
) {
    RestoreFlow(
        source = RestoreSource.SOFALOG,
        title = "Restore a sofalog backup",
        chooseLabel = "Choose a sofalog backup…",
        confirmText = "Every movie and show in this file is replaced with the file's state. Everything else on " +
            "this device stays.",
        onMenu = onMenu,
        onOpenMovie = onOpenMovie,
        onOpenShow = onOpenShow,
        onOpenSeason = onOpenSeason,
    ) {
        wide { BackupImportedCard() }
        wide { TmdbCard(onAbout) }
    }
}

/**
 * The page both restores share: pick the file, see what it holds, confirm, watch it run, read the
 * report. [intro] is what the page says before a job starts.
 */
@Composable
private fun RestoreFlow(
    source: RestoreSource,
    title: String,
    chooseLabel: String,
    confirmText: String,
    onMenu: () -> Unit,
    onOpenMovie: (Long) -> Unit,
    onOpenShow: (Long) -> Unit,
    onOpenSeason: (Long, Int) -> Unit,
    intro: LazyGridScope.() -> Unit,
) {
    val restore = restoreRepository()
    // A file picked on the other restore page is not this page's to import.
    LaunchedEffect(source) { restore.enter(source) }
    val state by restore.state.collectAsStateWithLifecycle()
    var confirmImport by remember { mutableStateOf(false) }
    var confirmStop by remember { mutableStateOf(false) }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri -> uri?.let { restore.pick(it, source) } }
    // Providers often mislabel a .json file, so the picker is not narrowed to one type.
    val choose = { picker.launch(arrayOf("application/json", "text/plain", "application/octet-stream")) }
    val running = (state as? RestoreState.Active)?.job?.state == RESTORE_RUNNING

    // The import pauses if the device sleeps, so the screen stays on while it runs.
    val view = LocalView.current
    DisposableEffect(running) {
        view.keepScreenOn = running
        onDispose { view.keepScreenOn = false }
    }

    DetailScaffold(title, onMenu, null, {}) { modifier ->
        LazyVerticalGrid(
            columns = GridCells.Adaptive(340.dp),
            modifier = modifier,
            contentPadding = PaddingValues(16.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            when (val s = state) {
                is RestoreState.Active ->
                    job(s.job, source == RestoreSource.SOFALOG, { confirmStop = true }, restore::discard, onOpenMovie, onOpenShow, onOpenSeason)
                else -> {
                    intro()
                    when (s) {
                        is RestoreState.Ready -> {
                            wide {
                                Card(Modifier.fillMaxWidth()) {
                                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                                        Fact("File", s.fileName)
                                        Fact("Exported", s.exportedAt?.let(::formatInstant) ?: "unknown")
                                        Fact("Watches", s.watches.toString())
                                        Fact("Ratings", s.ratings.toString())
                                        Fact("Watchlist", s.watchlist.toString())
                                        Fact("Favorites", s.favorites.toString())
                                        if (s.source == RestoreSource.SOFALOG) {
                                            Fact("Dropped shows", s.dropped.toString())
                                            Fact("Copies", s.copies.toString())
                                        }
                                    }
                                }
                            }
                            wide {
                                Button(onClick = { confirmImport = true }, modifier = Modifier.fillMaxWidth().height(56.dp)) { Text("Import") }
                            }
                            wide {
                                OutlinedButton(
                                    onClick = {
                                        restore.discard()
                                        choose()
                                    },
                                    modifier = Modifier.fillMaxWidth().height(56.dp),
                                ) { Text("Pick a different file") }
                            }
                        }
                        else -> {
                            val reading = s is RestoreState.Reading
                            wide {
                                Button(onClick = choose, enabled = !reading, modifier = Modifier.fillMaxWidth().height(56.dp)) {
                                    if (reading) {
                                        Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                                            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                                            Text("Reading the file…")
                                        }
                                    } else {
                                        Text(chooseLabel)
                                    }
                                }
                            }
                            (s as? RestoreState.Idle)?.notice?.takeIf { it.isNotEmpty() }?.let { notice ->
                                wide { Text(notice, color = MaterialTheme.colorScheme.error) }
                            }
                        }
                    }
                }
            }
        }
    }

    if (confirmImport) {
        ConfirmDialog(
            title = "Import this file?",
            text = confirmText,
            confirm = "Yes, import",
            onConfirm = {
                confirmImport = false
                restore.start()
            },
            onDismiss = { confirmImport = false },
        )
    }
    if (confirmStop) {
        ConfirmDialog(
            title = "Stop the import?",
            text = "What was imported so far stays. Running the same file again later completes it.",
            confirm = "Stop import",
            onConfirm = {
                confirmStop = false
                restore.stop()
            },
            onDismiss = { confirmStop = false },
        )
    }
}

/** Where the file comes from: the author's first app, and the one that took its place. */
@Composable
private fun TicketBoxStory() {
    Card(Modifier.fillMaxWidth()) {
        Column(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 20.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                MarkDisc(R.drawable.ticket_box, 38.dp, size = 56.dp)
                Icon(DetailIcons.ChevronRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                MarkDisc(R.drawable.sofalog_mark, 46.dp, size = 56.dp)
            }
            Text("From Ticket Box to Sofalog", style = MaterialTheme.typography.titleMedium)
            Text(
                "Ticket Box was where this started: the first app I built to keep track of what I watch. " +
                    "I have since moved on to Sofalog, and this is the app I work on now. If you used " +
                    "Ticket Box, you do not have to start over — export a backup there and bring your " +
                    "watch history, ratings, watchlist and favorites with you.",
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
}

/** What an import does to the library, stated before the file is picked. */
@Composable
private fun ImportedCard() {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("What gets imported", style = MaterialTheme.typography.titleSmall)
            SettingsNote(
                "Watch history, ratings, watchlist and favorites from the file are imported into " +
                    "your library. For every movie or show that has watches in the file, your " +
                    "existing sofalog watches for it are replaced by the file's. Ratings, watchlist " +
                    "entries and favorites are added or updated. Nothing else is touched.",
            )
        }
    }
}

/** What restoring the app's own backup does to the library, stated before the file is picked. */
@Composable
private fun BackupImportedCard() {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text("What gets restored", style = MaterialTheme.typography.titleSmall)
            SettingsNote(
                "Watch history, ratings, watchlist, favorites, dropped shows and your copies. For every " +
                    "movie or show in the file, what this device holds for it is replaced by what the file " +
                    "says — including removing what the file does not have. Movies and shows that are not " +
                    "in the file are not touched, so restoring the same file twice changes nothing.",
            )
            SettingsNote("Your settings and your TMDB account link are not part of a backup.")
        }
    }
}

/**
 * What TMDB is asked for during a restore. It explains the use and shows the logo; the attribution
 * notice itself lives on About alone, which the button leads to.
 */
@Composable
private fun TmdbCard(onAbout: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 4.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                // TMDB's official square logo, the form meant for small sizes: never tinted.
                Image(
                    painterResource(R.drawable.tmdb_logo_square),
                    contentDescription = "TMDB",
                    modifier = Modifier.padding(top = 2.dp).width(40.dp),
                )
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Text("Titles come from TMDB", style = MaterialTheme.typography.titleSmall)
                    SettingsNote(
                        "The backup names movies and shows by their TMDB id and holds nothing else about " +
                            "them. For each one not yet in your library, this device asks The Movie " +
                            "Database for its details, seasons, episodes and artwork, and stores them " +
                            "here — so a large file takes a while. Your watch history is never sent to TMDB.",
                    )
                }
            }
            TextButton(onClick = onAbout, modifier = Modifier.padding(start = 44.dp)) { Text("About TMDB and your data") }
        }
    }
}

private fun LazyGridScope.wide(content: @Composable () -> Unit) {
    item(span = { GridItemSpan(maxLineSpan) }) { content() }
}

@Composable
private fun Fact(label: String, value: String) {
    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(label, Modifier.weight(1f), color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, Modifier.weight(2f))
    }
}

private fun LazyGridScope.job(
    job: RestoreJob,
    ownBackup: Boolean,
    onStop: () -> Unit,
    onAnother: () -> Unit,
    onOpenMovie: (Long) -> Unit,
    onOpenShow: (Long) -> Unit,
    onOpenSeason: (Long, Int) -> Unit,
) {
    val running = job.state == RESTORE_RUNNING
    val r = job.report
    if (running) {
        wide {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(restoreProgressLine(job.progress), style = MaterialTheme.typography.titleMedium)
                LinearProgressIndicator(
                    progress = { restorePercent(job.progress) / 100f },
                    modifier = Modifier.fillMaxWidth().height(8.dp),
                )
                SettingsNote("${job.progress.groupsDone} of ${job.progress.groupsTotal} movies and shows · ${restorePercent(job.progress)}%")
                SettingsNote("Keep sofalog open until the import finishes.")
            }
        }
    } else {
        wide {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                if (job.state == RESTORE_DONE) {
                    Text("Import finished", style = MaterialTheme.typography.titleLarge)
                } else {
                    Text("Import failed", style = MaterialTheme.typography.titleLarge, color = MaterialTheme.colorScheme.error)
                    job.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
                }
                job.finishedAt?.let { SettingsNote("Finished ${formatInstant(it)}") }
            }
        }
    }
    // Dropped shows and copies exist in the app's own backup only.
    val own = if (ownBackup) listOf("Shows marked dropped" to r.showsDropped, "Copies added" to r.copiesInserted) else emptyList()
    val counters = listOf(
        "Watches added" to r.historyInserted,
        "Ratings set" to r.ratingsUpserted,
        "Watchlist entries added" to r.watchlistInserted,
        "Favorites added" to r.favoritesInserted,
    ) + own + listOf(
        "Movies added to the library (of ${r.moviesTotal} in the file)" to r.moviesMaterialised,
        "Shows added to the library (of ${r.showsTotal} in the file)" to r.showsMaterialised,
        "Existing watches replaced" to r.historyReplaced,
        "Skipped" to r.skippedTotal,
    )
    items(counters, key = { "counter:${it.first}" }) { (label, value) -> Counter(label, value, Modifier.fillMaxWidth()) }
    if (running) {
        wide { OutlinedButton(onClick = onStop, modifier = Modifier.fillMaxWidth().height(56.dp)) { Text("Stop import") } }
        return
    }
    wide { Text("Skipped", style = MaterialTheme.typography.titleMedium) }
    if (r.skippedTotal == 0) wide { SettingsNote("Nothing was skipped.") }
    items(r.skipped.size) { index ->
        SkipCard(r.skipped[index], onOpenMovie, onOpenShow, onOpenSeason)
    }
    if (r.skippedTotal > r.skipped.size) wide { SettingsNote("and ${r.skippedTotal - r.skipped.size} more") }
    wide { Button(onClick = onAnother, modifier = Modifier.fillMaxWidth().height(56.dp)) { Text("Import another file") } }
}

/** One skipped entry. Tapping it opens the page of the movie, show or season it belongs to. */
@Composable
private fun SkipCard(skip: RestoreSkip, onOpenMovie: (Long) -> Unit, onOpenShow: (Long) -> Unit, onOpenSeason: (Long, Int) -> Unit) {
    val showId = skip.showTmdbId ?: skip.tmdbId.takeIf { skip.type == "show" }
    val open: (() -> Unit)? = when {
        skip.reason == SKIP_MOVIE_NOT_FOUND || skip.reason == SKIP_SHOW_NOT_FOUND -> null
        skip.type == "movie" -> skip.tmdbId?.let { { onOpenMovie(it) } }
        showId != null && skip.season != null -> ({ onOpenSeason(showId, skip.season) })
        showId != null -> ({ onOpenShow(showId) })
        else -> null
    }
    Card(Modifier.fillMaxWidth().heightIn(min = 56.dp).then(if (open != null) Modifier.clickable(onClick = open) else Modifier)) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(skipKindLabel(skip.kind), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary)
            Text(skipTarget(skip), style = MaterialTheme.typography.titleSmall)
            Text(skipReasonLabel(skip.reason), fontWeight = FontWeight.SemiBold)
            skipReasonExplanation(skip)?.let { SettingsNote(it) }
        }
    }
}
