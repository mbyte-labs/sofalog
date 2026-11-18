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
import ro.mbyte.sofalog.ui.search.SearchScreen
import ro.mbyte.sofalog.ui.upnext.UpNextScreen

private const val UP_NEXT = "upnext"
private const val SEARCH = "search"
private const val ABOUT = "about"

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
        gesturesEnabled = route == UP_NEXT || route == SEARCH || drawer.isOpen,
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
                MovieScreen(it.arguments!!.getLong("id"), onBack = { nav.popBackStack() })
            }
            composable("show/{id}", listOf(navArgument("id") { type = NavType.LongType })) {
                val id = it.arguments!!.getLong("id")
                ShowScreen(id, onBack = { nav.popBackStack() }, onOpenSeason = { n -> nav.navigate("show/$id/season/$n") })
            }
            composable(
                "show/{id}/season/{n}",
                listOf(navArgument("id") { type = NavType.LongType }, navArgument("n") { type = NavType.IntType }),
            ) {
                SeasonScreen(it.arguments!!.getLong("id"), it.arguments!!.getInt("n"), onBack = { nav.popBackStack() })
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
                EpisodeScreen(
                    season = it.arguments!!.getInt("n"),
                    episode = it.arguments!!.getInt("e"),
                    onBack = { nav.popBackStack() },
                    onOpenShow = { nav.navigate("show/$id") },
                )
            }
        }
        // Composed AFTER the NavHost on purpose: the last back callback registered wins, and the
        // NavHost's own would otherwise pop the screen under an open drawer.
        BackHandler(enabled = drawer.isOpen) { scope.launch { drawer.close() } }
    }
}

private fun NavHostController.topLevel(route: String) = navigate(route) {
    popUpTo(graph.findStartDestination().id) { saveState = true }
    launchSingleTop = true
    restoreState = true
}
