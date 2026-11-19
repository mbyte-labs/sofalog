package ro.mbyte.sofalog.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.padding
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import ro.mbyte.sofalog.data.MediaKind
import ro.mbyte.sofalog.ui.about.DataSourcesScreen
import ro.mbyte.sofalog.ui.details.EpisodeScreen
import ro.mbyte.sofalog.ui.details.MovieScreen
import ro.mbyte.sofalog.ui.details.SeasonScreen
import ro.mbyte.sofalog.ui.details.ShowScreen
import ro.mbyte.sofalog.ui.history.HistoryScreen
import ro.mbyte.sofalog.ui.placeholder.ComingSoonScreen
import ro.mbyte.sofalog.ui.search.SearchScreen
import ro.mbyte.sofalog.ui.upnext.UpNextScreen

private const val UP_NEXT = "upnext"
private const val HISTORY = "history"
private const val SEARCH = "search"
private const val ABOUT = "about"
private const val TRENDING = "trending"
private const val SETTINGS = "settings"

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

    ModalNavigationDrawer(
        drawerState = drawer,
        // The edge swipe opens the menu on the top-level screens only; a detail page keeps its
        // plain back gesture. An open drawer can always be swiped shut.
        gesturesEnabled = route == UP_NEXT || route == HISTORY || route == SEARCH || drawer.isOpen,
        drawerContent = {
            ModalDrawerSheet {
                Text(
                    "sofalog",
                    style = MaterialTheme.typography.titleLarge,
                    modifier = Modifier.padding(horizontal = 28.dp, vertical = 20.dp),
                )
                val item = Modifier.padding(NavigationDrawerItemDefaults.ItemPadding)
                NavigationDrawerItem(
                    label = { Text("Up Next") },
                    icon = { Text("▶") },
                    selected = route == UP_NEXT,
                    onClick = { go { nav.topLevel(UP_NEXT) } },
                    modifier = item,
                )
                NavigationDrawerItem(
                    label = { Text("History") },
                    icon = { Text("↺") },
                    selected = route == HISTORY,
                    onClick = { go { nav.topLevel(HISTORY) } },
                    modifier = item,
                )
                NavigationDrawerItem(
                    label = { Text("Search") },
                    icon = { Text("⌕") },
                    selected = route == SEARCH,
                    onClick = { go { nav.topLevel(SEARCH) } },
                    modifier = item,
                )
                HorizontalDivider(Modifier.padding(horizontal = 28.dp, vertical = 8.dp))
                NavigationDrawerItem(
                    label = { Text("Data sources") },
                    selected = false,
                    onClick = { go { nav.navigate(ABOUT) { launchSingleTop = true } } },
                    modifier = item,
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
                    onTrending = { nav.navigate(TRENDING) { launchSingleTop = true } },
                    onSettings = { nav.navigate(SETTINGS) { launchSingleTop = true } },
                    onMenu = openMenu,
                )
            }
            // Placeholders: neither page is built, so each only says what it will be. Reached from
            // the empty Up Next page alone — the drawer gains its entries with the real pages.
            composable(TRENDING) {
                ComingSoonScreen(
                    title = "Trending",
                    text = "Trending movies and shows from TMDB will be listed here.",
                    onBack = { nav.popBackStack() },
                )
            }
            composable(SETTINGS) {
                ComingSoonScreen(
                    title = "Settings",
                    text = "This is where you will set the app up.",
                    onBack = { nav.popBackStack() },
                    planned = listOf("Sync with your TMDB account", "The country for where-to-watch listings"),
                )
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
            composable(SEARCH) {
                SearchScreen(
                    onOpen = { hit ->
                        nav.navigate(if (hit.kind == MediaKind.MOVIE) "movie/${hit.tmdbId}" else "show/${hit.tmdbId}")
                    },
                    onMenu = openMenu,
                )
            }
            composable(ABOUT) { DataSourcesScreen(onBack = { nav.popBackStack() }) }
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
