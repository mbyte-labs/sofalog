package ro.mbyte.sofalog.ui

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
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
    Column {
        Box(Modifier.weight(1f)) {
            NavHost(nav, startDestination = UP_NEXT) {
                composable(UP_NEXT) {
                    UpNextScreen(
                        onOpenShow = { nav.navigate("show/$it") },
                        onSearch = { nav.topLevel(SEARCH) },
                        onAbout = { nav.navigate(ABOUT) },
                    )
                }
                composable(SEARCH) {
                    SearchScreen(onOpen = { hit ->
                        nav.navigate(if (hit.kind == MediaKind.MOVIE) "movie/${hit.tmdbId}" else "show/${hit.tmdbId}")
                    })
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
            }
        }
        if (route == UP_NEXT || route == SEARCH) {
            NavigationBar {
                NavigationBarItem(
                    selected = route == UP_NEXT,
                    onClick = { nav.topLevel(UP_NEXT) },
                    icon = { Text("▶") },
                    label = { Text("Up Next") },
                )
                NavigationBarItem(
                    selected = route == SEARCH,
                    onClick = { nav.topLevel(SEARCH) },
                    icon = { Text("⌕") },
                    label = { Text("Search") },
                )
            }
        }
    }
}

private fun NavHostController.topLevel(route: String) = navigate(route) {
    popUpTo(graph.findStartDestination().id) { saveState = true }
    launchSingleTop = true
    restoreState = true
}
