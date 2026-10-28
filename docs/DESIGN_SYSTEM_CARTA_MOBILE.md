# Carta Mobile — Design System

The working reference for Carta's Android UI: every token, every component, and the rules that
connect them. It describes **what the code does today**, checked against
`android/app/src/main/res/` and `internal/ui/assets/`.

Carta Mobile has two layers. Read this document with that in mind.

- **Native chrome** (Kotlin and XML views on Material 3): a top bar, a status strip, the content
  area (survey, empty state or refusal) and the action bar; plus the baseline sheet and the drift
  snackbar.
- **The survey itself** (a WebView): the CLI's own UI from `internal/ui/assets`, loaded from the
  APK. There is one copy of it, shared with `carta ui`, so a change there changes both.

**Sources of truth, in order:**

1. `internal/ui/assets/app.css` `:root`: the colour tokens, light and dark
2. `android/app/src/main/res/values/colors.xml` and `values-night/colors.xml`: the same tokens for
   native views
3. `android/app/src/main/res/values/themes.xml`: the Material 3 role mapping, text, tag and button
   styles
4. This document: usage rules and the component catalogue

Colours are defined in three places, and all three must change together (§8).

---

## 1. What the system is for

A survey is a set of claims about a network, each based on evidence of a different quality. The
UI's job is to show how sure each claim is, and to show what was never looked at as missing
rather than empty. Every rule below follows from that.

| Principle | System consequence |
|---|---|
| **Certainty is shown by shape** | Map nodes are filled (identified), outlined (partly known) or dashed (answered, revealed nothing). Shape survives greyscale, colour blindness and a printout. |
| **Confidence is a word, never only a colour** | Every claim carries a `conf` badge reading `asserted`, `observed`, `inferred` or `weak`. The border style (solid, dashed, dotted) restates it. |
| **Unknown is drawn, not left blank** | The coverage bar hatches addresses that didn't answer and leaves addresses never reached open. Neither is drawn as a filled "empty" segment. |
| **The instrument states its own bias** | What a phone can't see (no ARP, no MACs) is shown in the survey's coverage and findings. The UI never hides it to look complete. |
| **Refusing is an answer** | When the app won't survey, the refusal fills the content area with a title, the reason and the next step. It is never an empty result or a line of dim text. |
| **Quiet voice** | Typographic, monospace for anything measured, no illustrations, no celebration. The brightest thing on screen is the one action to take. |

**Modern does not mean Material You.** Carta is built on Material 3, but wallpaper-derived colour,
shadows, gradients and celebratory motion each break a principle above. Colours mean state, so a
colour that depends on the wallpaper would change what they mean. Dynamic colour is therefore never
applied: no `DynamicColors` call exists, and none may be added.

---

## 2. Foundations

### 2.1 Colour

Every token has a light and a dark value. Dark mode follows the system setting:
`prefers-color-scheme` in the WebView and `values-night` for native views.

| Token | Light | Dark | Use |
|---|---|---|---|
| `bg` | `#FBFBF9` | `#0E0F12` | Window, page, empty and refusal states |
| `panel` | `#FFFFFF` | `#14161A` | WebView panel and sheet, baseline sheet |
| `fg` | `#16181D` | `#E8E8E3` | Primary text; identified nodes; primary button fill |
| `dim` | `#6B7280` | `#9AA0AA` | Secondary text, status strip, evidence, outline-button border |
| `rule` | `#E3E3DE` | `#24262C` | Hairlines and dividers only; never a control's boundary |
| `partial` / `accent` | `#0969DA` | `#58A6FF` | Partly-known nodes, infrastructure, `asserted` badge, focus ring |
| `accent_inverse` | `#58A6FF` | `#0969DA` | Actions on inverse surfaces (the snackbar's **View**) |
| `unknown` | `#B58900` | `#D9A441` | Dashed outline of unidentified nodes. **Never text in light mode** (§7) |
| `warn` | `#B54708` | `#E3A008` | Warning findings, drift caveats, the `STOPPED EARLY` tag, fingerprint fields that differ |
| `risk` | `#B42318` | `#FF7B72` | Risk findings, a failed survey in the status strip |

On Android `partial` is named `accent`. They are the same value.

**Material 3 roles.** `Theme.Carta` maps every role it uses to a Carta token:

| Material role | Carta token |
|---|---|
| `colorPrimary`, `colorOnSurface`, `colorOnBackground`, `colorOnPrimaryContainer` | `fg` |
| `colorOnPrimary`, `colorSurface`, `android:colorBackground`, `colorSurfaceContainerLowest` | `bg` |
| `colorPrimaryContainer`, `colorSurfaceContainer` (Low, High, Highest) | `panel` |
| `colorOutline`, `colorOnSurfaceVariant` | `dim` |
| `colorOutlineVariant` | `rule` |
| `colorSecondary`, `colorTertiary` | `accent` |
| `colorError` | `risk` |
| `colorSurfaceInverse` / `colorOnSurfaceInverse` / `colorPrimaryInverse` | `fg` / `bg` / `accent_inverse` |

A role left unmapped falls back to Material's baseline purple. That happened once, on the
snackbar's action, so every new Material component gets its roles checked in both themes.

**Rules**

- **Colour means state, never decoration.** `warn` and `risk` appear only on findings, caveats,
  tags and differences. `partial` appears only on things that are partly known or are
  infrastructure, and on the keyboard focus ring.
- **Depth comes from tone and lines, not shadow.** `bg`, `panel` and `rule` are the only depth
  cues. Nothing has a drop shadow; the sliding panel is separated from the map by a `rule` line.
- **The launcher icon background is always the light `bg`.** Launchers don't follow night mode,
  and the logo is drawn in dark strokes.

### 2.2 Typography

Two families. One is bundled; the other is the platform's.

- **JetBrains Mono** (OFL, licence at `assets/fonts/JetBrainsMono-OFL.txt`) carries everything
  measured. It is bundled because the platform's monospace differs everywhere and is Droid Sans
  Mono on Android, and Carta's identity rides on this face. Its taller x-height reads better at
  Carta's smallest sizes (0.62rem badges).
  - Android: one variable `.ttf` in `res/font`, exposed as `@font/jetbrains_mono` at weights 400
    and 700.
  - Web and `carta ui`: Regular and SemiBold `.woff2` in `internal/ui/assets/fonts/`, loaded by
    `@font-face` from `'self'`, so the CSP is unchanged. `--mono` names it first and keeps the
    system monospace fonts as fallbacks.
  - The map's canvas copies the `--mono` stack into `ctx.font` and redraws when the font
    finishes loading.
- **The platform sans** (Roboto on Android, `system-ui` on the web) carries prose and controls.

| Role | Style | Size | Where |
|---|---|---|---|
| Brand | `Carta.Brand`: mono bold, tracking 0.12 | 13 sp | `CARTA` in the top bar |
| Status | `Carta.Mono`: mono, `dim` | 12 sp | Status strip, up to three lines |
| Tag | `Carta.Tag`: mono uppercase, `warn`, hairline box | 10 sp | `STOPPED EARLY`, instrument tags |
| Button | sans, sentence case | 14 sp | **Survey**, **Stop**, **Compare**, refusal actions |
| Title | `Carta.Title`: sans bold | 20 sp | Empty and refusal states, baseline sheet |
| Body | `Carta.Body`: sans, `dim`, line spacing 1.3 | 15 sp | Explanations under a title |
| Web body | `--sans` | 0.85rem on a phone | Panels, findings, device details |
| Web data | `--mono` | 0.62–0.72rem | Addresses, prefixes, evidence, `conf` badges |
| Map labels | `--mono` on the canvas | 11px and 9px × text zoom | Node label and sub-label |

**Rules**

- **Measured values are always monospace:** addresses, prefixes, counts, survey IDs, the
  baseline fingerprint. Monospace text reads as data.
- **Buttons are sentence case.** Uppercase belongs to the brand mark, tags and `conf` badges.
- **Text follows the reader's text size everywhere.** Native text uses `sp`. The WebView already
  scales its text with Android's font size (measured at font scale 2.0), so `textZoom` is left at
  its default rather than applied twice. Canvas labels read the page's root font size and scale
  with it.

### 2.3 Spacing

Native chrome uses a 4 dp grid:

| Value | Use |
|---|---|
| 4 dp | Status strip top padding; gap between status and tag |
| 8 dp | Gap between buttons in landscape; bottom margin under titles |
| 10 dp | Action bar vertical padding; space after the mark |
| 12 dp | Gap between the action bar's buttons; gap between refusal text blocks |
| 16 dp | Horizontal padding of every strip; button horizontal padding |
| 24 dp | Empty, refusal and sheet content padding |

The WebView uses rem units from `app.css`. On a phone, list rows get `0.6rem` vertical padding,
so each one is a usable touch target.

### 2.4 Shape

| Element | Shape |
|---|---|
| Buttons | 4 dp corners (`Carta.Shape.Small`), overriding Material 3's pill shape; 1 dp stroke (outline) or solid fill (primary) |
| Tags | 2 dp corners, 1 dp `warn` stroke |
| `conf` badge | 2 px radius, 1 px border whose **style** carries the confidence |
| Bottom sheet, web sheet | 12 px top corners, the only rounded large surface |
| Map nodes | Circles, 9 px radius for devices, 13 px for infrastructure |
| Findings | No box; a 2 px left rule in `warn`/`risk`/`rule` |

### 2.5 Elevation

None. Strips are separated by 1 dp `rule` hairlines. Two surfaces float, both with a scrim and no
shadow of Carta's own: the baseline `BottomSheetDialog` and the snackbar.

### 2.6 Layout and sizing

**Native, portrait** (`layout/activity_main.xml`), top to bottom:

1. **Top bar**, 48 dp: the mark and `CARTA`, nothing else.
2. **Status strip** (`layout/status_strip.xml`): the status line in mono at full width, the
   `STOPPED EARLY` tag under it when the survey was cut short, and a 2 dp progress indicator
   along its bottom edge. The tag sits under the status, not beside it: beside it, at large font
   sizes, it took half the width and cut the status off.
3. **Content area** (`layout/content_area.xml`), exactly one of: the WebView, the empty state, or
   the refusal state.
4. **Action bar**: **Compare** (outlined) and **Survey/Stop** (filled, taking the remaining
   width), at the bottom where a thumb reaches.

**Native, landscape** (`layout-land/activity_main.xml`): one row of chrome — mark, brand, status
(one line), tag, **Compare**, **Survey** — then the progress indicator and the content. On a
phone on its side the thumbs are at the sides, and a bottom bar would cost the map its height. The
ids match the portrait layout, so the Activity doesn't know which it has.

- The app draws edge to edge (targetSdk 36 requires it). The root pads itself by the system-bar
  and display-cutout insets.
- **Minimum touch target is 48 × 48 dp.** Buttons have Material's 6 dp touch insets removed, so
  the visible button is the target and the bars don't grow.

**Web layouts.** `app.js` sets two classes on `<body>`:

| Layout | When | What changes |
|---|---|---|
| Desktop | wider than 760px, not embedded | Unchanged: header with brand, vantage and legend; map beside a 27rem panel; coverage under the map |
| `compact` | `?embedded`, or narrower than 760px | The vantage line moves into the panel's head; the legend moves into a card behind a **Legend** button on the map. With `?embedded` the page header is hidden entirely: the native bar and status strip already say what it said. |
| `sheet` | narrower than 760px | The map is full-bleed. The panel slides over it from the bottom (§5, Sheet). The coverage strip moves into the sheet's head, so it is always visible. |

A phone on its side in the app is `compact` without `sheet`: the side panel stays, the header is
gone, and the map has nearly the full height left by the native chrome. Its short height also
tightens the tabs and panel padding (`max-height: 520px`).

Elements are moved between places, never duplicated, so there is one vantage line, one legend and
one coverage strip whichever layout is showing.

### 2.7 Motion

- Map layout runs a short force simulation once, then stops. The same survey always produces the
  same picture, because nodes are seeded on a circle rather than at random.
- The web sheet eases to its snap height in 0.22s; while dragged it follows the finger exactly.
- Material's own motion only: the bottom sheet, the snackbar, button ripples. No other
  transitions.
- The progress indicator is **indeterminate** during stages with no count and **determinate**
  during counted stages, showing `done` of `total`.

### 2.8 Iconography and haptics

- **One mark:** `carta_logo`. In the bar it is tinted `fg`, so it works in both themes. In the
  WebView it is inverted in dark mode by CSS.
- **No icon set.** Actions are words. A network-survey tool's actions (**Survey**, **Stop**,
  **Compare**, **Legend**) don't have icons that are clearer than their names.
- **Launcher icon:** an adaptive icon with the mark inset 27% inside the 108 dp canvas, which
  keeps it in the 66 dp safe zone. The same drawable is the monochrome layer for themed icons.
- **Haptics:** one `CONFIRM` pulse when a survey finishes and one `REJECT` pulse when Carta
  refuses to survey (API 30+). They mark the two outcomes worth knowing without looking. They use
  `performHapticFeedback`, which follows the phone's own touch-feedback setting, so there is no
  in-app switch. They fire only on a change seen live, never on the first render after a
  rotation.

---

## 3. Theming

| Layer | Light/dark switch | Where tokens live |
|---|---|---|
| Native | `Theme.Material3.DayNight.NoActionBar`, one theme; colours from `values/` and `values-night/` | `colors.xml`, `themes.xml` |
| Web | `@media (prefers-color-scheme: dark)` | `app.css` `:root` |

The WebView inherits the system's dark-mode setting. Its background is set to `bg` before the
page loads, so there is no white flash in dark mode.

---

## 4. Components: native (`android/app/src/main/`)

| Component | Built from | Rules |
|---|---|---|
| **Top bar** | `LinearLayout` | The mark and `CARTA`. Nothing else is added to it in portrait. |
| **Status strip** | `Carta.Mono` `TextView` + `Carta.Tag` + `LinearProgressIndicator` | Carries the survey's state and nothing else: `Sweeping 66 of 254`; `Stopping…`; `6 devices on 10.0.2.0/24, at 23:26`; or `…, stopped during Sweeping` with the `STOPPED EARLY` tag. A failed survey shows in `risk`. A polite live region, so screen readers announce changes. Refusals and drift never write to it. |
| **Survey / Stop** | `MaterialButton`, `Carta.Button.Primary` | The only filled control. Reads **Stop** while running and **Stopping…** (disabled) until Go returns. Stopping keeps a partial result, as Ctrl-C does in the CLI. |
| **Compare** | `MaterialButton`, `Carta.Button` | Disabled until a survey exists and while one runs. Disabled reads as disabled: 38% text and a `rule` stroke, from colour state lists. |
| **Progress** | `LinearProgressIndicator`, 2 dp, `fg` on `rule` | Invisible (not gone) when idle, so the layout never jumps. Hidden for the instant it switches between determinate and indeterminate, which it refuses to do while shown. |
| **Empty state** | Title + body | Says what a phone can't see before the first survey, not after. |
| **Refusal state** | Title (heading) + reason + next step + action + **Show last survey** | Fills the content area. One per reason: VPN on (action: VPN settings), mobile data, no IPv4, not connected (action: the system Wi-Fi panel). **Show last survey** appears when there is one, and returns to it. |
| **Baseline sheet** | `BottomSheetDialog` (`layout/sheet_baselines.xml`, `item_baseline.xml`) | The hint sits beside the choice it is about. One row per earlier survey: date, a mono fingerprint (`net`, `gw`, `dhcp`, `dns`, device count), and tags for `STOPPED EARLY` and a different instrument. **Fields that differ from the survey on screen are in `warn`**; a difference is evidence of another network, a match proves nothing. Each row's spoken description says which fields differ. Opens fully expanded in landscape. **Clear comparison** appears while one is shown. |
| **Drift snackbar** | Material `Snackbar` | After a comparison: the one-line summary and **View**, which opens the Drift tab. Anchored above the action bar in portrait. Shown once per comparison, not again on rotation. |

---

## 5. Components: survey UI (`internal/ui/assets/`)

These are shared with `carta ui`. A change here appears on the desktop too.

| Component | Rules |
|---|---|
| **Map node** | Filled `named` = identified. Outlined `partial` = partly known. Dashed `unknown` = answered, revealed nothing. `infra` (blue fill, larger) = gateway, resolver, DHCP server. |
| **Map gestures** | Pointer events: drag a node, pan with one finger, pinch with two, wheel on desktop. `touch-action: none` gives every gesture on the canvas to the map. Touch hit slop is 18 px (6 px for a mouse). |
| **Map, for assistive tech** | `role="img"` with a label rebuilt for each survey: "Map of N devices on PREFIX. The All tab lists them." A **Skip the map** link, off-screen until focused, is the first stop in the keyboard order and opens the All tab with its filter focused. |
| **Legend** (compact) | A **Legend** button at the map's top right opens a card with the four shape keys. Back, or the button again, closes it. |
| **Sheet** (narrow) | The panel slides over the map, snapping to **peek** (handle, vantage, coverage, tabs), **half** or **full**. Only the grip — the handle and the head — drags it, so the map keeps pan and pinch and the panel's body scrolls. On release it settles at the nearest height, or one step on in the direction of a flick. Choosing a tab or a device from peek raises it to half, and a selected node is panned clear of it. The map is framed above the peek, so nothing starts hidden. The handle is a real button: tap or Enter cycles the heights, arrows step them, and its label and `aria-expanded` say which. The panel sits above the map's own overlays. |
| **Back** (in the app) | `window.carta.collapse()` closes the legend, then lowers the sheet to peek, and reports whether it did anything. The app calls it on Back and leaves only when nothing was open. The page never calls the app. |
| **Coverage bar** | Three segments: answered (solid `named`), swept but silent (hatched), never reached (open). Under the map, or in the sheet's head; never over the map. Widths are set through the DOM, never an inline style, so the CSP holds. The sentence under it gives all three counts. |
| **Tabs** | `role="tablist"`; each tab is `role="tab"` with `aria-selected`; only the selected tab is in the Tab order, and arrows, Home and End move between them. Panels are `role="tabpanel"`. |
| **`conf` badge** | Uppercase mono. `asserted` is solid `partial`; `observed` is solid `fg`; `inferred` is dashed; `weak` is dotted at 80% opacity. |
| **Finding** | Title, `conf` badge, detail with preserved line breaks. Left rule in `risk`/`warn`/`rule` by severity. |
| **Device panel** | Name, addresses, then a claims table where every value has its badge, then services, notes, disagreements and merges. "Not learned" is printed, never left blank. Receives focus when a device is opened from the list or the keyboard. |
| **List row** | `role="button"`, in the Tab order, opened with Enter or Space. Label on the left, addresses and class in mono on the right, with a labelled filter box above. |
| **Drift tab** | Hidden unless `api/drift` answers; opened directly when the page is loaded with `?tab=drift`. Caveats come first, in `warn`. If the surveys aren't comparable, a bold caveat says so before any change is listed. Each change shows **how** it was matched (`by mac, observed`, `by host, inferred`, `by ip, weak`). |

---

## 6. Patterns

**Refusals.** `OrientationProvider` returns a reason kind and one sentence of why. The Activity
adds a title, the next step and the action for that kind. The refusal replaces the content area;
the status strip keeps describing the survey still on file.

**Cut-short surveys.** **Stop**, the 4-minute deadline and the ViewModel being cleared all
produce the same thing: a partial survey whose coverage names the stage it stopped in, and in
which only addresses actually reached count as swept. The status strip reads *"3 devices on
10.0.2.0/24, stopped during Sweeping"* with the `STOPPED EARLY` tag. The coverage bar leaves the
unreached range open.

**Drift.** The operator picks the baseline; the app never chooses it. The sheet shows the
fingerprint fields because they are the only clues available, and marks the ones that differ. The
comparison repeats any mismatch as a caveat, and a change matched by address alone is always shown
as `weak`.

**Untrusted text.** Hostnames, banners, mDNS names and page titles are whatever the devices on
the network chose to send. Rules:
- In `app.js`, every value goes through `esc()` before it touches `innerHTML`. Text that needs
  no markup uses `textContent`.
- `index.html` carries a CSP (`default-src 'self'`, no inline script or style).
- The WebView has no JavaScript bridge, no file or content access, and refuses any request or
  navigation outside the bundled pages. `evaluateJavascript` is used only for the app to call the
  page (Back, the snackbar's **View**).

**Data the WebView reads.** It reads `api/survey` and `api/drift`, served from memory by the
asset loader with `Cache-Control: no-store`. After a new survey or comparison the page is
reloaded, never patched. The page is loaded as `index.html?embedded=1`, with `&tab=drift` after
a comparison.

---

## 7. Accessibility

Measured WCAG contrast ratios (text needs 4.5:1, UI graphics 3:1). The ratios depend on colour
alone; JetBrains Mono's heavier strokes and taller x-height only make the smallest sizes easier to
read.

| Pair | Light | Dark |
|---|---|---|
| `fg` on `bg` | 17.1 | 15.6 |
| `dim` on `bg` | 4.67 | 7.29 |
| `dim` on `panel` | 4.83 | 6.89 |
| `partial` on `bg` | 5.01 | 7.59 |
| `warn` on `bg` | 5.24 | 8.49 |
| `risk` on `bg` | 6.35 | 7.60 |
| `unknown` on `bg` | **3.10** | 8.52 |
| `rule` on `bg` | 1.24 | 1.27 |
| Primary button (`bg` on `fg`) | 17.1 | 15.6 |

- **`unknown` in light mode passes only as a graphic.** It is used for the dashed node stroke and
  the legend dot, never for text.
- **`rule` is decorative.** It is for hairlines, the coverage hatching and a disabled button's
  stroke, never an enabled control's boundary. The outline button's border is `dim` for this
  reason.
- Confidence and certainty never rely on colour: badges are words, and nodes differ in fill and
  stroke style.
- **Screen readers** get: the status strip as a polite live region; refusal and sheet titles as
  headings; spoken descriptions on baseline rows that name the fields that differ; the map's label
  and the skip link; proper tab and tab-panel roles.
- **Keyboard** (desktop and hardware keyboards): skip link first; tabs with arrows; rows with
  Enter; focus moves to the device panel when a device opens; a visible `partial` focus ring.
- **Text size:** checked at Android's largest font scale (2.0). Native text, web text and map
  labels all scale; the status strip wraps rather than truncating.
- Every native button is at least 48 × 48 dp.

---

## 8. Adding to the system

1. **A new colour** goes in `app.css` (both themes), `colors.xml` and `values-night/colors.xml`
   in the same commit, with a row in §2.1 and its contrast in §7. If it fails 4.5:1 against `bg`
   or `panel`, say what it may not be used for.
2. **A new Material component** gets its colour roles checked in both themes: an unmapped role
   shows Material's purple. Map the role in `Theme.Carta`, never on the component.
3. **A new native component** uses the `Carta.*` styles or the theme's colours, never a literal
   colour. It needs a row in §4.
4. **A change to the survey UI** is a change to `carta ui` too. Check it at desktop width, at
   375×812 and at 914×411, embedded and not, in both themes. Any value from the survey goes
   through `esc()`. Anything movable between layouts is moved, not copied.
5. **Before shipping:** build in light and dark, at the largest font size, in both orientations,
   and rotate during a survey: the survey must keep running and its progress must reappear.

---

## 9. Drift and debt

Known gaps between this system and the code, to fix rather than copy:

- **The WebView container has no accessible name of its own.** Its content is fully labelled, but
  `uiautomator` flags the container itself. Naming it risks TalkBack reading the name instead of
  the page; it needs checking with TalkBack on a real phone before changing.
- **The baseline sheet lists every survey ever taken.** There is no delete and no grouping, so on
  a phone used for months it will grow long.
- **A refusal does not clear itself** when the network changes. After joining Wi-Fi the operator
  taps **Survey** again; the screen does not notice on its own.
- **Not verified on a physical phone:** the sheet's drag feel, pinch alongside it, haptics, and
  TalkBack. The emulator and synthetic pointer events covered the logic, not the feel.
