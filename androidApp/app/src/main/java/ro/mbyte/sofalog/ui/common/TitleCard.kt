package ro.mbyte.sofalog.ui.common

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ro.mbyte.sofalog.data.MediaKind
import ro.mbyte.sofalog.data.SearchHit
import ro.mbyte.sofalog.domain.LibraryMark
import ro.mbyte.sofalog.ui.details.parts.DetailIcons
import ro.mbyte.sofalog.ui.details.parts.HeartRed
import ro.mbyte.sofalog.ui.theme.NeutralBadge
import ro.mbyte.sofalog.ui.theme.OnBadge
import ro.mbyte.sofalog.ui.theme.UserRatingBadge
import ro.mbyte.sofalog.ui.theme.WatchedItemColor
import ro.mbyte.sofalog.ui.theme.WatchlistItemColor

/**
 * One TMDB title as a poster — the Search and Discover grids' card. The corners: top-left a row of small tabs saying what the library
 * holds (watched, dropped, on the watchlist, in the favorites), the user's own rating top-right, the kind
 * bottom-left and TMDB's score bottom-right.
 */
@Composable
fun TitleCard(hit: SearchHit, mark: LibraryMark, onClick: () -> Unit) {
    val movie = hit.kind == MediaKind.MOVIE
    Column(Modifier.clickable(onClick = onClick), horizontalAlignment = Alignment.CenterHorizontally) {
        Box(Modifier.clip(PosterShape)) {
            Poster(hit.posterPath, hit.title, Modifier.fillMaxWidth())
            // One tab per state that holds, in the detail pages' order and colours.
            Row(Modifier.align(Alignment.TopStart).clip(RoundedCornerShape(bottomEnd = 6.dp))) {
                if (mark.watched) MarkTab(DetailIcons.CheckBold, "Watched", WatchedItemColor)
                // Neutral on purpose — the kind badge's background, as on the show page's top bar.
                if (mark.dropped) MarkTab(DetailIcons.Dropped, "Dropped", NeutralBadge)
                if (mark.watchlisted) MarkTab(DetailIcons.Bookmark, "On your watchlist", WatchlistItemColor)
                if (mark.favorited) MarkTab(DetailIcons.Heart, "In your favorites", HeartRed)
            }
            // One colour for both kinds — the score badge's, its twin in the opposite corner.
            PosterBadge(
                if (movie) "Movie" else "Show",
                NeutralBadge,
                OnBadge,
                RoundedCornerShape(topEnd = 6.dp),
                Modifier.align(Alignment.BottomStart),
            )
            mark.userRating?.let {
                PosterBadge(
                    it.toString(),
                    UserRatingBadge,
                    OnBadge,
                    RoundedCornerShape(bottomStart = 6.dp),
                    Modifier.align(Alignment.TopEnd).semantics { contentDescription = "Your rating $it" },
                )
            }
            hit.voteAverage?.takeIf { it > 0 }?.let {
                PosterBadge(
                    "♥ %.1f".format(it),
                    NeutralBadge,
                    OnBadge,
                    RoundedCornerShape(topStart = 6.dp),
                    Modifier.align(Alignment.BottomEnd),
                )
            }
        }
        Text(
            hit.title,
            style = MaterialTheme.typography.titleSmall,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth().padding(top = 6.dp),
        )
        hit.year?.let {
            Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}
