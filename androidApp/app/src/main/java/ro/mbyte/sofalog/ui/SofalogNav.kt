package ro.mbyte.sofalog.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import ro.mbyte.sofalog.BuildConfig
import ro.mbyte.sofalog.data.ListKey
import ro.mbyte.sofalog.data.MediaKind
import ro.mbyte.sofalog.ui.about.AboutScreen
import ro.mbyte.sofalog.ui.details.EpisodeScreen
import ro.mbyte.sofalog.ui.details.MovieScreen
import ro.mbyte.sofalog.ui.details.SeasonScreen
import ro.mbyte.sofalog.ui.details.ShowScreen
import ro.mbyte.sofalog.ui.history.HistoryScreen
import ro.mbyte.sofalog.ui.lists.ListScreen
import ro.mbyte.sofalog.ui.lists.ListsScreen
import ro.mbyte.sofalog.ui.discover.DiscoverScreen
import ro.mbyte.sofalog.ui.common.AppLogo
import ro.mbyte.sofalog.ui.common.tmdbAccount
import ro.mbyte.sofalog.ui.details.parts.DetailIcons
import ro.mbyte.sofalog.ui.search.SearchScreen
import ro.mbyte.sofalog.ui.settings.AccountScreen
import ro.mbyte.sofalog.ui.settings.RegionPickerScreen
import ro.mbyte.sofalog.ui.settings.DeleteDataScreen
import ro.mbyte.sofalog.ui.settings.RestoreScreen
import ro.mbyte.sofalog.ui.settings.SettingsScreen
import ro.mbyte.sofalog.ui.settings.TicketBoxRestoreScreen
import ro.mbyte.sofalog.ui.settings.TmdbAccountScreen
import ro.mbyte.sofalog.ui.settings.ZonePickerScreen
import ro.mbyte.sofalog.ui.upnext.UpNextScreen

private const val UP_NEXT = "upnext"
private const val HISTORY = "history"
private const val SEARCH = "search"
// Lists is a drawer root; each list's page sits under it so the drawer keeps Lists highlighted.
private const val LISTS = "lists"
private val ListKey.route: String get() = "$LISTS/${name.lowercase()}"
// About is a Settings sub-page: its route sits under "settings" so the drawer highlights Settings.
private const val ABOUT = "settings/about"
private const val DISCOVER = "discover"
private const val SETTINGS = "settings"
private const val SETTINGS_ACCOUNT = "settings/account"
private const val SETTINGS_ZONE = "settings/timezone"
private const val SETTINGS_REGION = "settings/region"
private const val SETTINGS_RESTORE = "settings/restore"
private const val SETTINGS_DELETE = "settings/delete"
private const val SETTINGS_TICKET_BOX = "settings/restore/ticket-box"
private const val SETTINGS_TMDB = "settings/tmdb"

/** Titles are addressed by TMDB id everywhere: it names tracked and untracked items alike. */
@Composable
fun SofalogNav() {
    val nav = rememberNavController()
    val entry by nav.currentBackStackEntryAsState()
    val route = entry?.destination?.route
    val drawer = rememberDrawerState(DrawerValue.Closed)
    val scope = rememberCoroutineScope()
    val openMenu: () -> Unit = { scope.launch { drawer.open() } }
    // Every drawer entry closes the drawer and then goes where it points.
    fun go(navigate: () -> Unit) {
        scope.launch { drawer.close() }
        navigate()
    }
    fun push(route: String) = nav.navigate(route) { launchSingleTop = true }

    // TMDB's approval page came back through the app's link: show the page that finishes it.
    val linkReturns by tmdbAccount().returns.collectAsStateWithLifecycle()
    LaunchedEffect(linkReturns) {
        if (linkReturns > 0) push(SETTINGS_TMDB)
    }

    ModalNavigationDrawer(
        drawerState = drawer,
        // The edge swipe opens the menu on the top-level screens only; a detail page keeps its
        // plain back gesture. An open drawer can always be swiped shut.
        gesturesEnabled = route == UP_NEXT || route == HISTORY || route == LISTS || route == SEARCH || route == DISCOVER || route == SETTINGS || drawer.isOpen,
        drawerContent = {
            ModalDrawerSheet(Modifier.widthIn(max = 300.dp)) {
                DrawerContent(
                    route = route,
                    onUpNext = { go { nav.topLevel(UP_NEXT) } },
                    onHistory = { go { nav.topLevel(HISTORY) } },
                    onLists = { go { nav.topLevel(LISTS) } },
                    onSearch = { go { nav.topLevel(SEARCH) } },
                    onDiscover = { go { nav.topLevel(DISCOVER) } },
                    onSettings = { go { nav.topLevel(SETTINGS) } },
                )
            }
        },
    ) {
        NavHost(nav, startDestination = UP_NEXT) {
            composable(UP_NEXT) {
                UpNextScreen(
                    onOpenEpisode = { show, season, episode ->
                        nav.navigate("show/$show/season/$season/episode/$episode")
                    },
                    onSearch = { nav.topLevel(SEARCH) },
                    onTrending = { nav.topLevel(DISCOVER) },
                    onSettings = { nav.topLevel(SETTINGS) },
                    onMenu = openMenu,
                )
            }
            // Always live from TMDB (D-034); a card opens the title by TMDB id, as a search result does.
            composable(DISCOVER) {
                DiscoverScreen(
                    onOpen = { hit ->
                        nav.navigate(if (hit.kind == MediaKind.MOVIE) "movie/${hit.tmdbId}" else "show/${hit.tmdbId}")
                    },
                    onMenu = openMenu,
                )
            }
            // Settings is a drawer root; its sub-pages are pushed above it. Every one carries the
            // menu button, and the system back gesture is the way back, as on a detail page.
            composable(SETTINGS) {
                SettingsScreen(
                    onMenu = openMenu,
                    onAbout = { push(ABOUT) },
                    onRestore = { push(SETTINGS_RESTORE) },
                    onDelete = { push(SETTINGS_DELETE) },
                    onTmdb = { push(SETTINGS_TMDB) },
                    onAccount = { push(SETTINGS_ACCOUNT) },
                )
            }
            composable(SETTINGS_ACCOUNT) {
                AccountScreen(
                    onMenu = openMenu,
                    onPickZone = { push(SETTINGS_ZONE) },
                    onPickRegion = { push(SETTINGS_REGION) },
                )
            }
            composable(SETTINGS_ZONE) { ZonePickerScreen(onMenu = openMenu, onDone = { nav.popBackStack() }) }
            composable(SETTINGS_REGION) { RegionPickerScreen(onMenu = openMenu, onDone = { nav.popBackStack() }) }
            composable(SETTINGS_RESTORE) {
                RestoreScreen(onMenu = openMenu, onTicketBox = { push(SETTINGS_TICKET_BOX) })
            }
            composable(SETTINGS_TICKET_BOX) {
                TicketBoxRestoreScreen(
                    onMenu = openMenu,
                    onAbout = { push(ABOUT) },
                    onOpenMovie = { id -> nav.navigate("movie/$id") },
                    onOpenShow = { id -> nav.navigate("show/$id") },
                    onOpenSeason = { id, n -> nav.navigate("show/$id/season/$n") },
                )
            }
            // After everything is deleted the app starts over on its first page, with nothing above it.
            composable(SETTINGS_DELETE) {
                DeleteDataScreen(onMenu = openMenu, onDeleted = { nav.topLevel(UP_NEXT) })
            }
            composable(SETTINGS_TMDB) {
                TmdbAccountScreen(onMenu = openMenu, onAbout = { push(ABOUT) })
            }
            composable(HISTORY) {
                HistoryScreen(
                    onOpenMovie = { id -> nav.navigate("movie/$id") },
                    onOpenEpisode = { show, season, episode ->
                        nav.navigate("show/$show/season/$season/episode/$episode")
                    },
                    onMenu = openMenu,
                )
            }
            // The Lists page and one page per list, pushed above it like a Settings sub-page.
            composable(LISTS) { ListsScreen(onMenu = openMenu, onOpen = { push(it.route) }) }
            for (key in ListKey.entries) {
                composable(key.route) {
                    ListScreen(
                        key,
                        onMenu = openMenu,
                        onOpenMovie = { id -> nav.navigate("movie/$id") },
                        onOpenShow = { id -> nav.navigate("show/$id") },
                        onSearch = { nav.topLevel(SEARCH) },
                    )
                }
            }
            composable(SEARCH) {
                SearchScreen(
                    onOpen = { hit ->
                        nav.navigate(if (hit.kind == MediaKind.MOVIE) "movie/${hit.tmdbId}" else "show/${hit.tmdbId}")
                    },
                    onMenu = openMenu,
                )
            }
            composable(ABOUT) { AboutScreen(onMenu = openMenu) }
            composable("movie/{id}", listOf(navArgument("id") { type = NavType.LongType })) {
                MovieScreen(
                    it.arguments!!.getLong("id"),
                    onMenu = openMenu,
                    onOpenMovie = { id -> nav.navigate("movie/$id") },
                )
            }
            composable("show/{id}", listOf(navArgument("id") { type = NavType.LongType })) {
                val id = it.arguments!!.getLong("id")
                ShowScreen(
                    id,
                    onMenu = openMenu,
                    onOpenSeason = { n -> nav.navigate("show/$id/season/$n") },
                    onOpenShow = { other -> nav.navigate("show/$other") },
                )
            }
            composable(
                "show/{id}/season/{n}",
                listOf(navArgument("id") { type = NavType.LongType }, navArgument("n") { type = NavType.IntType }),
            ) {
                val id = it.arguments!!.getLong("id")
                val n = it.arguments!!.getInt("n")
                SeasonScreen(
                    id,
                    n,
                    onMenu = openMenu,
                    onOpenEpisode = { e -> nav.navigate("show/$id/season/$n/episode/$e") },
                    // Back to the show when that is where this page was opened from; a fresh one otherwise.
                    onOpenShow = {
                        val under = nav.previousBackStackEntry
                        if (under?.destination?.route == "show/{id}" && under.arguments?.getLong("id") == id) {
                            nav.popBackStack()
                        } else {
                            nav.navigate("show/$id")
                        }
                    },
                )
            }
            composable(
                "show/{id}/season/{n}/episode/{e}",
                listOf(
                    navArgument("id") { type = NavType.LongType },
                    navArgument("n") { type = NavType.IntType },
                    navArgument("e") { type = NavType.IntType },
                ),
            ) {
                val id = it.arguments!!.getLong("id")
                val n = it.arguments!!.getInt("n")
                EpisodeScreen(
                    id,
                    n,
                    it.arguments!!.getInt("e"),
                    onMenu = openMenu,
                    onOpenShow = { nav.navigate("show/$id") },
                    // Back to the season when that is where this page was opened from; a fresh one otherwise.
                    onOpenSeason = {
                        val under = nav.previousBackStackEntry
                        val args = under?.arguments
                        if (under?.destination?.route == "show/{id}/season/{n}" && args?.getLong("id") == id && args.getInt("n") == n) {
                            nav.popBackStack()
                        } else {
                            nav.navigate("show/$id/season/$n")
                        }
                    },
                )
            }
        }
        // Composed AFTER the NavHost on purpose: the last back callback registered wins, and the
        // NavHost's own would otherwise pop the screen under an open drawer.
        BackHandler(enabled = drawer.isOpen) { scope.launch { drawer.close() } }
    }
}

// A drawer entry always lands on the page it names: whatever was open above Up Next is dropped,
// never saved and restored (the bottom-tab recipe, which would reopen a detail page here).
private fun NavHostController.topLevel(route: String) = navigate(route) {
    popUpTo(graph.findStartDestination().id)
    launchSingleTop = true
}

/**
 * The drawer's content: the brand, the pages, and — pinned to the bottom, as on the web sidebar —
 * Settings and the version. Only the upper part scrolls, so Settings stays reachable in landscape.
 */
@Composable
private fun DrawerContent(
    route: String?,
    onUpNext: () -> Unit,
    onHistory: () -> Unit,
    onLists: () -> Unit,
    onSearch: () -> Unit,
    onDiscover: () -> Unit,
    onSettings: () -> Unit,
) {
    Column(Modifier.fillMaxHeight()) {
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            DrawerHeader()
            HorizontalDivider(
                Modifier.padding(horizontal = 24.dp),
                color = MaterialTheme.colorScheme.outlineVariant,
            )
            DrawerSectionLabel("Video")
            DrawerEntry("Up Next", DetailIcons.Play, route == UP_NEXT, onUpNext)
            DrawerEntry("History", DetailIcons.History, route == HISTORY, onHistory)
            DrawerEntry("Lists", DetailIcons.Bookmark, route?.startsWith(LISTS) == true, onLists)
            DrawerEntry("Search", DetailIcons.Search, route == SEARCH, onSearch)
            DrawerEntry("Discover", DetailIcons.Trending, route == DISCOVER, onDiscover)
        }
        HorizontalDivider(
            Modifier.padding(horizontal = 24.dp, vertical = 8.dp),
            color = MaterialTheme.colorScheme.outlineVariant,
        )
        DrawerEntry("Settings", DetailIcons.Settings, route?.startsWith(SETTINGS) == true, onSettings)
        Text(
            "Version ${BuildConfig.VERSION_NAME}",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(start = 28.dp, end = 28.dp, top = 12.dp, bottom = 16.dp),
        )
    }
}

@Composable
private fun DrawerHeader() {
    Row(
        Modifier.padding(start = 24.dp, end = 24.dp, top = 24.dp, bottom = 20.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        AppLogo(size = 44.dp, corner = 12.dp)
        Column(Modifier.padding(start = 14.dp)) {
            Text("sofalog", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.SemiBold)
            Text(
                "Your movies & shows",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun DrawerSectionLabel(text: String) {
    Text(
        text.uppercase(),
        style = MaterialTheme.typography.labelMedium,
        letterSpacing = 1.2.sp,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 28.dp, end = 28.dp, top = 20.dp, bottom = 8.dp),
    )
}

@Composable
private fun DrawerEntry(label: String, icon: ImageVector, selected: Boolean, onClick: () -> Unit) {
    NavigationDrawerItem(
        label = { Text(label, fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal) },
        // No description: the label beside it already names the entry.
        icon = { Icon(icon, contentDescription = null) },
        selected = selected,
        onClick = onClick,
        modifier = Modifier.padding(NavigationDrawerItemDefaults.ItemPadding),
    )
}
