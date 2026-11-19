package ro.mbyte.sofalog.ui.theme

import androidx.compose.ui.graphics.Color

val Purple80 = Color(0xFFD0BCFF)
val PurpleGrey80 = Color(0xFFCCC2DC)
val Pink80 = Color(0xFFEFB8C8)

/*
 * Up Next poster tabs. Every tab carries the same white text. The Airs amber is a shade deeper
 * than a pure yellow so the white stays legible on it; both finales share one red.
 */
val AirsBadge = Color(0xFFD9A21B)
val FinaleBadge = Color(0xFFC0413F)
val SeasonPremiereBadge = Color(0xFF3A9A5B)
val OnBadge = Color.White

/** The user's own rating on a history poster — the web card's pill colour. */
val UserRatingBadge = Color(0xFFE8435A)

/** The watched item colour — the web's --accent. Backs the top bar's white ✓ on a watched title. */
val WatchedItemColor = Color(0xFF0EA5E9)

/** The top bar's watchlist mark — the web's --cta. */
val WatchlistItemColor = Color(0xFF7C5CFF)

/**
 * The quiet badge background: a search poster's kind label, score and dropped tab, and the top
 * bar's dropped mark. Dropped is deliberately neutral — a state, not an alarm.
 */
val NeutralBadge = Color.Black.copy(alpha = 0.6f)
