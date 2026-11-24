import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
    alias(libs.plugins.ksp)
}

// The TMDB key is a build-time secret (SPEC D-040): `tmdb.apiKey` in the gitignored
// local.properties, or the SOFALOG_TMDB_API_KEY environment variable. Never committed. A build
// without one still works; the app says the key is missing wherever it would call TMDB.
val localProps = Properties().apply {
    val file = rootProject.file("local.properties")
    if (file.exists()) file.inputStream().use(::load)
}

fun secret(property: String, env: String): String =
    (localProps.getProperty(property) ?: System.getenv(env) ?: "").trim()

val tmdbApiKey: String = secret("tmdb.apiKey", "SOFALOG_TMDB_API_KEY")

// The release keystore, likewise never committed: four `release.*` entries in local.properties,
// or the SOFALOG_KEYSTORE_* environment variables. The same key signs the GitHub APK and is the
// app signing key on Google Play, so an install can move between the two. Without a keystore the
// release build is left unsigned rather than failing — debug builds never need one.
val releaseStoreFile: String = secret("release.storeFile", "SOFALOG_KEYSTORE_FILE")
val releaseStorePassword: String = secret("release.storePassword", "SOFALOG_KEYSTORE_PASSWORD")
val releaseKeyAlias: String = secret("release.keyAlias", "SOFALOG_KEYSTORE_KEY_ALIAS")
val releaseKeyPassword: String = secret("release.keyPassword", "SOFALOG_KEYSTORE_KEY_PASSWORD")
val hasReleaseKeystore: Boolean = releaseStoreFile.isNotEmpty()

android {
    namespace = "ro.mbyte.sofalog"
    compileSdk {
        version = release(37)
    }

    defaultConfig {
        applicationId = "ro.sofalog"
        minSdk = 35
        targetSdk = 37
        versionCode = 1
        versionName = "0.5"

        buildConfigField("String", "TMDB_API_KEY", "\"$tmdbApiKey\"")

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    signingConfigs {
        if (hasReleaseKeystore) {
            create("release") {
                storeFile = rootProject.file(releaseStoreFile)
                storePassword = releaseStorePassword
                keyAlias = releaseKeyAlias
                keyPassword = releaseKeyPassword
            }
        }
    }

    buildTypes {
        release {
            if (hasReleaseKeystore) signingConfig = signingConfigs.getByName("release")
            optimization {
                enable = true
                packageScope = setOf("androidx.**", "kotlin.**", "kotlinx.**")
            }
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_11
        targetCompatibility = JavaVersion.VERSION_11
    }
    buildFeatures {
        compose = true
        buildConfig = true
    }
}

dependencies {
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.activity.compose)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.navigation.compose)
    implementation(libs.androidx.room.runtime)
    ksp(libs.androidx.room.compiler)
    implementation(libs.coil.compose)
    implementation(libs.coil.network.okhttp)
    implementation(libs.kotlinx.coroutines.android)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.okhttp)
    testImplementation(libs.junit)
    androidTestImplementation(platform(libs.androidx.compose.bom))
    androidTestImplementation(libs.androidx.compose.ui.test.junit4)
    androidTestImplementation(libs.androidx.espresso.core)
    androidTestImplementation(libs.androidx.junit)
    debugImplementation(libs.androidx.compose.ui.test.manifest)
    debugImplementation(libs.androidx.compose.ui.tooling)
}