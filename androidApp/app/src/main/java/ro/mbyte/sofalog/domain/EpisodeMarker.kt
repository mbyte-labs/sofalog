package ro.mbyte.sofalog.domain

/** The Up Next card's episode marker — the server's marker vocabulary, ported. */
enum class EpisodeMarker(val label: String) {
    SEASON_PREMIERE("Season Premiere"),
    MID_SEASON_FINALE("Mid-Season Finale"),
    SEASON_FINALE("Season Finale"),
}

/**
 * The server's `EpisodeMarker` (common/service_season_details.go), case for case: TMDB's
 * `episode_type` decides a finale, and episode 1 of any season is its premiere. Null is a plain
 * episode — including every row fetched before the type was stored.
 */
fun episodeMarker(episodeType: String?, episodeNumber: Int): EpisodeMarker? = when {
    episodeType == "finale" -> EpisodeMarker.SEASON_FINALE
    episodeType == "mid_season" -> EpisodeMarker.MID_SEASON_FINALE
    episodeNumber == 1 -> EpisodeMarker.SEASON_PREMIERE
    else -> null
}
