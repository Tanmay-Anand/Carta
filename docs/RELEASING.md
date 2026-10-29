# Carta for Android — Releasing and Updating

How a change gets from this repository onto the phone.

Every change ships the same way: a new APK on GitHub Releases, which Obtainium on the phone picks
up and installs with one tap. There is no over-the-air path. The UI lives inside the APK, just as
`carta ui` embeds it in the binary, and the app fetches nothing from the internet at runtime.

---

## 1. One-time setup

### The signing key

Every APK must be signed with the same key, or Android refuses to install it over the previous
one. The key is yours: it is generated on your machine and never committed.

```bash
keytool -genkeypair -v -keystore carta-release.jks -alias carta -keyalg RSA -keysize 4096 -validity 36500
```

**Back the `.jks` file and its passwords up somewhere safe.** If they are lost, the next release
cannot install over the app on your phone. The only way out is to uninstall it, which deletes
every survey stored on the phone.

### GitHub secrets

On GitHub, open **Carta → Settings → Secrets and variables → Actions → New repository secret**,
and add:

| Secret | Value |
|---|---|
| `CARTA_KEYSTORE_B64` | The keystore, base64-encoded (command below) |
| `CARTA_KEYSTORE_PASSWORD` | The keystore password |
| `CARTA_KEY_ALIAS` | `carta`, or the alias you chose |
| `CARTA_KEY_PASSWORD` | The key password |

To encode the keystore for `CARTA_KEYSTORE_B64`, run this in PowerShell, in the folder that holds
the keystore. `Resolve-Path` matters: .NET resolves a bare file name against the process's own
directory (often `C:\Windows\System32`), not the folder PowerShell is in.

```powershell
[Convert]::ToBase64String([IO.File]::ReadAllBytes((Resolve-Path "carta-release.jks"))) | Set-Clipboard
```

On macOS or Linux, run this:

```bash
base64 -w0 carta-release.jks
```

### Signing local builds (optional)

To build a signed release on your machine, create `android/keystore.properties`. It is gitignored.

```properties
storeFile=C:/path/to/carta-release.jks
storePassword=…
keyAlias=carta
keyPassword=…
```

Local and CI builds use the same key, so either APK installs over the other.

---

## 2. Each release

1. Set the new version in **`android/version.properties`** (for example `versionName=0.2.0`).
   `versionCode` is derived from it, so there is nothing else to bump.
2. Rename the `## Unreleased` section of `RELEASE_NOTES.md` to `## v0.2.0`. It becomes the GitHub
   release text.
3. Commit, then tag and push:

   ```bash
   git tag v0.2.0
   ```

   ```bash
   git push origin main v0.2.0
   ```

`.github/workflows/release-android.yml` then:

- checks that the tag matches `version.properties`
- runs the Go tests for the core and the binding
- builds the `.aar` from the Go sources and the APK around it, for arm64
- verifies the signature
- publishes `carta-v0.2.0.apk` as a GitHub Release

Obtainium picks the release up on its next check.

**Never reuse or move a tag.** Obtainium compares the release tag with the installed version. A
release whose tag doesn't match the APK's version makes it offer the same update forever.

**Tags are shared with the CLI.** If the CLI starts publishing its own releases, attach its
binaries to the same release rather than tagging separately. Obtainium only installs the `.apk`,
but a release without one would look like the latest version to it.

---

## 3. Obtainium on the phone (once)

[Obtainium](https://github.com/ImranR98/Obtainium) is a free, open-source Android app that installs
and updates apps straight from their GitHub releases.

1. Install Obtainium from its GitHub releases page or F-Droid.
2. On the phone, open this link. It opens Obtainium with Carta filled in:

   https://apps.obtainium.imranr.dev/redirect?r=obtainium://app/%7B%22id%22%3A%22io.github.tanmayanand.carta%22%2C%22url%22%3A%22https%3A%2F%2Fgithub.com%2FTanmay-Anand%2FCarta%22%2C%22author%22%3A%22Tanmay-Anand%22%2C%22name%22%3A%22Carta%22%7D

   Or add it manually: **Add App** → paste `https://github.com/Tanmay-Anand/Carta` → **Add**.
3. Allow Obtainium to install apps when Android asks. That's the one-time "Install unknown apps"
   permission.

From then on: Obtainium notifies you → tap **Update** → confirm Android's install prompt. Surveys
are kept, because an update installs over the existing app.

**If an update fails with "App not installed" or a signature conflict,** the installed APK was
signed with a different key, usually a debug build from `gradlew installDebug`. Uninstall it, then
install from Obtainium. Uninstalling deletes the stored surveys.

---

## 4. Building locally

Install the toolchain once:

```bash
go install golang.org/x/mobile/cmd/gomobile@latest golang.org/x/mobile/cmd/gobind@latest
```

```bash
gomobile init
```

Install the Android SDK with NDK `28.2.13676358`, the version pinned in `android/app/build.gradle.kts`.

Then, from `android/`:

```bash
./gradlew installDebug
```

The `bindGo` task rebuilds `app/libs/carta.aar` from the Go sources whenever any of them changed,
and skips it when none did. You never build the `.aar` by hand, and it is not committed.

**Windows notes.**
- A space in the user directory (`C:\Users\First Last`) breaks cgo. `bindGo` passes the 8.3
  short form of the SDK path to work around it.
- If Gradle fails with *Unable to establish loopback connection*, Java could not create its Unix
  socket in the temp directory. Run Gradle with:

  ```powershell
  $env:JAVA_TOOL_OPTIONS = "-Djdk.net.unixdomain.tmpdir=C:\tmp"
  ```

---

## 5. Before raising targetSdk to 37

Android 17 enforces the local-network permission for apps that target API 37. Below 37, Android
grants `ACCESS_LOCAL_NETWORK` implicitly to any app that holds `INTERNET`. Carta already declares
the permission in its manifest. When raising `targetSdk`:

1. Request `ACCESS_LOCAL_NETWORK` at runtime before the first survey. It appears to the user in
   the Nearby devices group.
2. Treat a denial like a refusal from `OrientationProvider`: say why, and don't survey.
3. Test on an Android 17 phone. Without the grant, connections time out and the survey finds
   nothing, which would look like an empty network.
