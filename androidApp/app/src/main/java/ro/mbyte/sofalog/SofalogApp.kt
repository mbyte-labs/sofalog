package ro.mbyte.sofalog

import android.app.Application
import okhttp3.OkHttpClient
import ro.mbyte.sofalog.data.VideoRepository
import ro.mbyte.sofalog.data.db.AppDatabase
import ro.mbyte.sofalog.data.tmdb.TmdbClient
import ro.mbyte.sofalog.domain.Clock

/** Holds the app's few long-lived objects. Wiring is by hand; there is no DI framework. */
class SofalogApp : Application() {
    val repository: VideoRepository by lazy {
        VideoRepository(
            db = AppDatabase.open(this),
            tmdb = TmdbClient(BuildConfig.TMDB_API_KEY, OkHttpClient()),
            clock = Clock.System,
        )
    }
}
