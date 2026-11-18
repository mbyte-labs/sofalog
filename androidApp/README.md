# sofalog for Android

The Android app: a Gradle project of its own, written in Kotlin with Jetpack Compose, for Android
phones and tablets. It works on its own, and can be linked to a sofalog server through `/api/v1` —
the contract is [`api/openapi.yaml`](../api/openapi.yaml). Open **this directory** in Android Studio, not the
repository root.

**It is a plain Android app, not a Kotlin Multiplatform project.** There is no shared/common
module, no iOS or desktop target, and none is planned: code is written against the Android SDK and
AndroidX directly.

**It is still a hello-world shell**, but its scope is decided — [`SPEC.md`](../SPEC.md) D-040:
video only, local-first with its own database and its own TMDB access, usable standalone or linked
to a server (an outbox of commands pushed on open, then the server's snapshot replacing local
state). No background work, notifications or widgets.

## Layout

```
androidApp/
  settings.gradle.kts        the Gradle project (rootProject "Sofalog"), one module: :app
  build.gradle.kts           root build file: plugin declarations only
  gradle/libs.versions.toml  every dependency and plugin version, in one place
  app/                       the application module — the whole app lives here
    build.gradle.kts         applicationId ro.sofalog, namespace ro.mbyte.sofalog, SDK levels
    src/main/                AndroidManifest.xml, Kotlin sources (ro.mbyte.sofalog), resources
    src/test/                JVM unit tests
    src/androidTest/         instrumented tests (need a device or an emulator)
```

## Stack

- Kotlin, built with the Android Gradle Plugin's built-in Kotlin support plus the Compose compiler
  plugin.
- Jetpack Compose with Material 3 for all UI, versions pinned by the Compose BOM. No XML layouts
  and no Fragments.
- A single activity (`MainActivity`), edge to edge.
- `minSdk` 35, `targetSdk` / `compileSdk` 37.

## Rules

- The app works with no server at all; when linked, it depends on the server only through
  `/api/v1`. It never reads the database, the `data/`
  volume, or the Go or Svelte sources, and nothing under `server/` or `frontend/` depends on it.
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
