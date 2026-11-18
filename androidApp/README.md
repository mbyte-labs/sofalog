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
show and season pages, tracking a show, marking movies and episodes watched (now), the whole-show
and whole-season bulk marks, removing a watch, and the data-sources screen with the TMDB notice.
An Up Next card opens the episode page on a tap — a placeholder for now — and on a long-press
opens a bottom sheet offering "Mark as watched" and "Check in" (also a placeholder: it writes
nothing). "Mark as watched" there asks for an optional rating (1–10, stored in
`user_video_ratings`) and when the watch happened: now, the release date (midnight that day in the
device's zone) or a picked date and time, never in the future.
Not yet: the episode page's content, check-in, showing or editing ratings, dated watches anywhere
but that sheet, lists, history, upcoming, stats, discover, dropping a show, the
backup file, and everything about linking to a server.

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
- `user_show_states` has one writer: `VideoRepository.recompute`, in the same transaction as the
  watch-log change it follows.
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
