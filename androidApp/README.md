# sofalog for Android

The Android app: a Gradle project of its own, written in Kotlin with Jetpack Compose, for Android
phones and tablets. It works on its own, and can be linked to a sofalog server through `/api/v1` —
the contract is [`api/openapi.yaml`](../api/openapi.yaml). Open **this directory** in Android Studio, not the
repository root.

**It is a plain Android app, not a Kotlin Multiplatform project.** There is no shared/common
module, no iOS or desktop target, and none is planned: code is written against the Android SDK and
AndroidX directly.

**Its scope is [`SPEC.md`](../SPEC.md) D-040**: video only, local-first with its own database and its
own TMDB access, usable standalone or linked to a server (an outbox of commands pushed on open,
then the server's snapshot replacing local state). No background work, notifications or widgets.

**What exists today is the first slice of phase 1 (standalone):** Up Next, TMDB search, movie,
show, season and episode pages, tracking a show, marking movies and episodes watched (now), the whole-show
and whole-season bulk marks, removing a watch, and the data-sources screen with the TMDB notice.
An Up Next card opens the episode page on a tap and on a long-press
opens a bottom sheet offering "Mark as watched" and "Check in" (also a placeholder: it writes
nothing). "Mark as watched" there asks for an optional rating (1–10, stored in
`user_video_ratings`) and when the watch happened: now, the release date (midnight that day in the
device's zone) or a picked date and time, never in the future.
With nothing in progress, Up Next shows the app's mark (gently animated), a line that tells a
never-used library ("Your sofa is ready") from a caught-up one ("All caught up" — the log holds a
watch), and three cards: Search, Trending on TMDB and Settings. The last two open placeholder
pages (`ui/placeholder/ComingSoonScreen.kt`); neither is in the menu yet.
The History page lists every watch, one card per watch (a rewatch is another card), in month
sections of the device's zone, with a movies / episodes filter, newest- or oldest-first order and
48 more per "Load more"; a card opens the movie page or the episode page. It shows the
user's rating and, for movies, TMDB's score — an episode card has no score, because the device
stores none per season. A long-press on a card offers removal, each confirmed first: that one
entry; every entry of the movie or episode (offered when there is more than one); and for an
episode, every watch of its season or of its whole show.
The movie page is the web page's, section for section: a hero over the blurred poster, outbound
links and production facts, TMDB's score beside the user's own rating (set and cleared in place),
owned copies and where it streams (one country, `WATCH_REGION` = RO), the history section, cast, a
media gallery (backdrops, videos, posters) and similar movies. The history section is one block: a
summary (how many watches, how long since the last), a vertical timeline newest-first — each entry
its day, its time unless it was logged "on its release date", and its age in calendar days of the
device's zone; a log of five or more folds to three behind "Show all" — and under it the
"Mark As Watched" / "Log another watch" button with the watchlist and favorite toggles. From 840dp
the summary and the buttons sit beside the timeline. The page is laid out for touch: every
choice is a bottom sheet, the gallery is a full-screen viewer you swipe and pinch, and a tap on a
timeline entry asks before removing it. Everything beyond the row's columns is parsed per view
out of the stored TMDB body (`domain/MovieSnapshot.kt`); a tracked movie refetches after 48 hours
or when its body predates a section the page reads, and keeps rendering the old one when TMDB is
down. Rating, watchlisting, favoriting and adding a copy are tracking actions — they store an
untracked movie first. Cast cards are not links yet: there is no person page.
Its app bar lies over the hero — clear at the top, solid with the title once the hero has scrolled
away — and carries the menu; leaving the page is the system back. Its right end reports the
movie's state in up to three read-only marks — ✓ watched, a bookmark for the watchlist, a heart for
favorites — each drawn only while it holds.
The show page is the movie page's twin and the web show page's, section for section: the same hero
("Created by", years, season and episode counts), links and facts (network, status, air dates,
next episode), TMDB's score beside the user's rating and their episode average, copies with
per-kind season coverage ("2 of 5 seasons" — a show is owned per season, so a show-wide add or
remove fans out over every non-special season), streaming, "Your progress" (status, aired counts,
**Drop show / Resume show**), the episode watch timeline with the main button and the two
toggles, the aggregate cast, a season grid (progress, the user's average and TMDB's score on each
poster; as many columns as the window holds), media and similar shows. "Mark As Watched" opens the
bulk form: the remaining episodes or a full rewatch, watched now, at one moment, on each episode's
release date or on each season's premiere (with a fallback date for a season that has none).
Both pages are built from the same sections (`ui/details/parts/`), which take plain values rather
than a movie or a show; `domain/ShowSnapshot.kt` is the show's read of its stored body, sharing
the movie's video, image, streaming and similar-title rules. The ✓ in a show's app bar means
finished — every aired episode — not started.
The season page is the web's (`SeasonDetailsPage.svelte`) in the same sections: the hero with the
show's title above the season's (a way back to the show), a "you dropped this show" banner, TMDB's
season score beside "Your rating" — the average of the user's episode ratings, since a season is
not itself rated — this season's own copies (Specials included) and the show's streaming listing,
the episode grid (stills with ✓, the user's rating, TMDB's score and the premiere / finale marker;
two columns on a phone, as many as fit on a tablet), the season's watch history with removal and
the season-wide "Mark As Watched" (the bulk form, narrowed to the season; not offered for
Specials), and the season's cast with episode counts within it. A tap on an episode opens its
page; a long-press offers "Mark as watched" — the dated form, with a rating — as a shortcut past
it. The cast and the scores come from the season's own TMDB document (`aggregate_credits` and
`credits` appended), kept in `seasons.metadata_json` compacted to what the season and episode
pages read (`domain/SeasonSnapshot.kt`): the full body repeats the crew under every episode, so
that is dropped and each guest star is kept as name, character, photo and billing order. A season
stored before that column existed — or before the regulars and guests were kept — fills in on its
first view, and refreshes on view after 48h.

The episode page is the web's (`EpisodeDetailsPage.svelte`) in the same sections, and reads only
what the season page already holds — it makes no TMDB call of its own. The episode's still is the
band under the app bar (an episode has no poster), with the show and the season above the title
as two ways back, then `S01E05`, the air date, the runtime and the premiere / finale marker. Below
it: the "you dropped this show" banner, TMDB's episode score and vote count beside the user's
rating (an episode is rated directly, with or without a watch), the season's copies and the show's
streaming listing — both read-only here: a copy tile explains that copies are per season and
leads to the season page — the episode's own watch history with removal and "Mark As Watched" /
"Log another watch" (the dated form, with a rating), the season's regular cast, and the episode's
guest stars. Rating or marking an episode of an untracked show tracks the whole show first.
Not yet: check-in, the
watchlist, favorites and dropped list pages, the person page, upcoming, stats, discover and
trending, settings, the backup file, and everything about linking to a server.

## TMDB key

The app calls TMDB itself, with a key supplied at build time and never committed. Put it in the
gitignored `local.properties`:

```
tmdb.apiKey=<a TMDB v3 API key, or a v4 read access token>
```

or set `SOFALOG_TMDB_API_KEY` in the environment. A build without a key still runs; it says the
key is missing wherever it would call TMDB.

## Layout

```
androidApp/
  settings.gradle.kts        the Gradle project (rootProject "Sofalog"), one module: :app
  build.gradle.kts           root build file: plugin declarations only
  gradle/libs.versions.toml  every dependency and plugin version, in one place
  app/                       the application module — the whole app lives here
    build.gradle.kts         applicationId ro.sofalog, namespace ro.mbyte.sofalog, SDK levels
    src/main/                AndroidManifest.xml, Kotlin sources (ro.mbyte.sofalog), resources
      …/domain/              the rules ported from the server: pure Kotlin, no Android
      …/data/db/             the Room database (catalog tables + user_ state)
      …/data/tmdb/           the TMDB client
      …/data/                VideoRepository: every read and write the UI makes
      …/ui/                  Compose screens, one package per screen, and navigation
    src/test/                JVM unit tests, including the shared rule fixtures
    src/androidTest/         instrumented tests (need a device or an emulator)
```

## Stack

- Kotlin, built with the Android Gradle Plugin's built-in Kotlin support plus the Compose compiler
  plugin.
- Jetpack Compose with Material 3 for all UI, versions pinned by the Compose BOM. No XML layouts
  and no Fragments.
- A single activity (`MainActivity`), edge to edge, with navigation-compose.
- Room (KSP) for the database, OkHttp + kotlinx.serialization for TMDB, Coil for images.
  ViewModels are wired by hand; there is no DI framework.
- `minSdk` 35, `targetSdk` / `compileSdk` 37.

## Rules

- The app works with no server at all; when linked, it depends on the server only through
  `/api/v1`. It never reads the database, the `data/`
  volume, or the Go or Svelte sources, and nothing under `server/` or `frontend/` depends on it.
- The rules in `domain/` are ports of the server's (status, the aired predicate, the Up Next
  pick). They are held to the Go implementation by the JSON cases in
  [`fixtures/video-rules/`](../fixtures/video-rules/), which `RuleFixturesTest` and the server's Go
  tests both run: change a rule on one side, change the fixture and the other side with it.
  The Up Next card's episode marker (`domain/EpisodeMarker.kt`, the server's `EpisodeMarker`) is
  a display rule with only Kotlin tests.
- The database is unreleased but devices already hold data, so a schema change bumps the Room
  version with a real `Migration` — never a destructive fallback.
- The UI reads only the local database, through `VideoRepository`. TMDB is called for search, for
  previews of untracked titles and to materialise a title when it is first tracked.
- `user_show_states` has one system writer: `VideoRepository.recompute`, in the same transaction
  as the watch-log change it follows. The only other write is the user's own drop / un-drop
  (`setShowDropped`): an UPDATE that never creates a row, and an un-drop that recomputes at once.
- Phones and tablets are both first-class: a screen adapts to the window size it is given rather
  than assuming a phone in portrait.
- Every dependency version lives in `gradle/libs.versions.toml`; adding a dependency requires
  asking first, like a Go module or an npm package.
- The server image never builds this directory (`.dockerignore`), and `/verify` does not build it.
- Signing keystores are never committed.

## Commands

```
Build               cd androidApp && ./gradlew assembleDebug
Unit tests          cd androidApp && ./gradlew test
Instrumented tests  cd androidApp && ./gradlew connectedAndroidTest
Lint                cd androidApp && ./gradlew lint
```
