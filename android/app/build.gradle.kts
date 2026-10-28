import java.util.Properties
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// The version lives in one file and the release workflow checks the tag
// against it. versionCode is derived so that it cannot be forgotten.
val appVersion: String = Properties()
    .apply { rootProject.file("version.properties").inputStream().use { load(it) } }
    .getProperty("versionName")
require(appVersion.matches(Regex("""\d+\.\d+\.\d+"""))) { "version.properties: versionName must be MAJOR.MINOR.PATCH, got $appVersion" }
val appVersionCode: Int = appVersion.split(".").map(String::toInt).let { (major, minor, patch) -> major * 10000 + minor * 100 + patch }

// Release signing: keystore.properties on a developer machine, environment
// variables in CI. Neither is committed. Without them a release build is
// produced unsigned, which is fine for checking that it builds and useless for
// installing — Obtainium updates only install over the same key.
val keystoreProps = Properties().apply {
    rootProject.file("keystore.properties").takeIf { it.exists() }?.inputStream()?.use { load(it) }
}
fun signing(key: String, env: String): String? = keystoreProps.getProperty(key) ?: System.getenv(env)
val releaseStore: String? = signing("storeFile", "CARTA_KEYSTORE_PATH")

val repoRoot: File = rootProject.projectDir.parentFile
val aar: File = file("libs/carta.aar")

android {
    namespace = "io.github.tanmayanand.carta"
    compileSdk = 36
    ndkVersion = "28.2.13676358"

    defaultConfig {
        applicationId = "io.github.tanmayanand.carta"
        minSdk = 26
        // 36, not 37: Android 17 enforces the local-network permission for apps
        // that target it. Raising this means requesting ACCESS_LOCAL_NETWORK at
        // runtime first (docs/RELEASING.md).
        targetSdk = 36
        versionCode = appVersionCode
        versionName = appVersion
    }

    signingConfigs {
        if (releaseStore != null) {
            create("release") {
                storeFile = file(releaseStore)
                storePassword = signing("storePassword", "CARTA_KEYSTORE_PASSWORD")
                keyAlias = signing("keyAlias", "CARTA_KEY_ALIAS")
                keyPassword = signing("keyPassword", "CARTA_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release")
            // Phones are arm64. The x86_64 library in the .aar is for the
            // emulator, and shipping it would nearly double the download.
            ndk { abiFilters += "arm64-v8a" }
        }
    }

    // The UI is the CLI's own UI, read from where the CLI embeds it. There is
    // one copy, so the phone and `carta ui` cannot drift apart.
    sourceSets["main"].assets.srcDir(repoRoot.resolve("internal/ui/assets"))

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin {
    compilerOptions { jvmTarget.set(JvmTarget.JVM_17) }
}

// gomobile bind, run by Gradle whenever a Go source changes. Declaring the Go
// sources as inputs is what keeps the .aar from going stale: Gradle skips the
// task when nothing changed and reruns it when anything did, so a build can
// never pair new Kotlin with an old core.
val bindGo by tasks.registering(Exec::class) {
    description = "Builds libs/carta.aar from the Go core with gomobile bind."
    group = "build"
    inputs.files(
        fileTree(repoRoot.resolve("internal")) { include("**/*.go") },
        fileTree(repoRoot.resolve("mobile")) { include("**/*.go", "go.mod", "go.sum") },
        repoRoot.resolve("go.mod"), repoRoot.resolve("go.sum"),
    )
    outputs.file(aar)
    workingDir = repoRoot.resolve("mobile")

    doFirst {
        val sdk = androidComponents.sdkComponents.sdkDirectory.get().asFile
        val ndk = sdk.resolve("ndk/${android.ndkVersion}")
        check(ndk.isDirectory) { "NDK ${android.ndkVersion} is not installed under $sdk" }
        val goBin = File(command("go", "env", "GOPATH")).resolve("bin")
        val exe = if (isWindows()) ".exe" else ""
        executable = goBin.resolve("gomobile$exe").path
        check(File(executable!!).exists()) {
            "gomobile not found in $goBin — run: go install golang.org/x/mobile/cmd/gomobile@latest golang.org/x/mobile/cmd/gobind@latest && gomobile init"
        }
        aar.parentFile.mkdirs()
        args(
            "bind",
            "-target=android/arm64,android/amd64",
            "-androidapi", "26",
            "-javapkg", "io.github.tanmayanand.carta",
            "-trimpath",
            "-ldflags=-s -w",
            "-o", aar.path,
            "./core",
        )
        // gomobile hands these paths to cgo, which splits the compiler path on
        // spaces; a Windows user directory with a space in it breaks the build.
        // The 8.3 short form of the same directory has none.
        environment("ANDROID_HOME", shortPath(sdk))
        environment("ANDROID_NDK_HOME", shortPath(ndk))
        environment("PATH", goBin.path + File.pathSeparator + System.getenv("PATH"))
    }
}

tasks.named("preBuild") { dependsOn(bindGo) }

fun isWindows() = System.getProperty("os.name").startsWith("Windows")

fun command(vararg cmd: String): String =
    ProcessBuilder(*cmd).redirectErrorStream(true).start().inputStream.bufferedReader().readText().trim()

fun shortPath(f: File): String {
    if (!isWindows() || ' ' !in f.path) return f.path
    return command("cmd", "/c", "for %I in (\"${f.path}\") do @echo %~sI").ifEmpty { f.path }
}

dependencies {
    implementation(files(aar))
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.activity:activity-ktx:1.9.0")
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-ktx:2.7.0")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.7.0")
    implementation("androidx.webkit:webkit:1.12.1")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1")
}
