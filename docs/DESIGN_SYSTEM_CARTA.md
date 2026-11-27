# Carta — Design System

The reference for every Carta screen: the desktop app, `carta ui` in a browser, and the Android
app. It covers the tokens, the components and the rules that connect them.

Carta 1 moves all three surfaces to one design, called **D** while it was being drawn: a calm,
near-black instrument. The mocks it was drawn from are in [`desktop/mocks/`](../desktop/mocks/):
`d-combined.html` (desktop and wide browser) and `e-phone.html` (phone and narrow browser). They
work when opened, and they are the reference wherever this document is silent.

| Surface | Built with | Status |
|---|---|---|
| Desktop app | Go and Fyne (`desktop/`) | Design D |
| `carta ui` and the Android app's survey view | `internal/ui/assets` (HTML, CSS, JS) | Design D |
| Android native parts | Kotlin and XML views (`android/app/src/main/`) | Design D |

The design before D is described by the previous version of this document
(`DESIGN_SYSTEM_CARTA_MOBILE.md`, in git history).

---

## 1. What the system is for

A survey is a set of claims about a network, each based on evidence of a different quality. The
UI's job is to show how sure each claim is, and to show what was never looked at as missing rather
than empty. Every rule below follows from that.

| Principle | Consequence |
|---|---|
| **Certainty is shown by shape** | Map devices are filled (identified), outlined (partly known) or dashed (answered, said nothing). Shape survives greyscale, colour blindness and a printout. |
| **Confidence is a word, never only a colour** | Every claim prints `asserted`, `observed`, `inferred` or `weak` beside it. |
| **Unknown is drawn, not left blank** | The coverage matrix has a dot for every address: answered, asked but silent, or never reached. "Not learned" is printed where a value is missing. |
| **The instrument states its own bias** | What a vantage could not see (a phone has no ARP cache; a quiet survey sends nothing) is said in the coverage and the findings. |
| **Refusing is an answer** | When Carta will not survey, the refusal says why and what to do next. It is never an empty result. |
| **One quiet voice** | Black, white and grey. Large light type for the one number that matters, monospace for anything measured, red only for the selected device and for risk. No illustrations, gradients, shadows or celebration. |

---

## 2. Foundations

### 2.1 Colour

Every token has a dark and a light value. All three surfaces follow the system's light/dark
setting. Dark is the design's home; light is the same layout with the tones inverted.

| Token | Dark | Light | Use |
|---|---|---|---|
| `bg` | `#000000` | `#FAFAF8` | Window and page |
| `panel` | `#0B0B0B` | `#FFFFFF` | The phone's sheet; dialogs |
| `fg` | `#F0F0ED` | `#111111` | Primary text; identified devices; the selected tab's underline; the Survey pill |
| `dim` | `#8B8B87` | `#62625E` | Secondary text, evidence lines, "not learned", unidentified device labels, confidence words other than asserted/observed, the never-reached ring |
| `faint` | `#4A4A47` | `#B4B4AE` | **Decoration only:** the silent dots of the coverage matrix, the info finding's rule, the sheet grip |
| `hair` | `#1D1D1C` | `#E4E4DF` | Hairlines between columns, rows and bars |
| `thread` | `#2A2A28` | `#CFCFCA` | Map links |
| `red` | `#FF3B2F` | `#D92D20` | The selected device and its ring, the risk finding's rule, the Findings count, Stop |
| `amber` | `#D29B3C` | `#A87A00` | The dashed outline of an unidentified device. **Graphic only, never text** |
| `warn` | `#E3A008` | `#B54708` | Warning findings, drift caveats, the `STOPPED EARLY` tag |

**Rules**

- **Colour means state, never decoration.** `red` marks one thing at a time: what is selected, or
  what is a risk. `amber` and `warn` appear only on unidentified devices, warnings and caveats.
- **No accent colour.** Infrastructure is told apart by size, not hue. The previous blue is gone.
- **Depth is tone and hairlines.** `bg`, `panel` and `hair` are the only depth cues. Nothing has a
  shadow or a gradient.
- **Android never uses dynamic colour.** A colour that depends on the wallpaper would change what
  colours mean.

### 2.2 Typography

Two bundled families, so every surface looks the same:

- **Inter** (OFL) carries prose, controls and the headline. Light (300) for the headline,
  Regular (400) for everything else.
- **JetBrains Mono** (OFL) carries everything measured: addresses, MACs, prefixes, counts,
  evidence, confidence words, the brand mark and the small caps labels.

| Role | Family and weight | Desktop | Phone | Where |
|---|---|---|---|---|
| Headline count | Inter Light | 56 | 40 | "14 devices" |
| Headline network | Inter Light, `dim` | 22 | 17 | "on 192.168.10.0/24" |
| Collapsed headline | Inter Light | 22 / 15 | 22 / 15 | Phone, with the sheet open |
| Device name | Inter Light | 26 | 22 | Device tab heading |
| Body and values | Inter Regular | 14–15 | 14–15 | Claims, findings, list rows, buttons |
| Label | Inter Regular, `dim` | 11.5 | 11.5 | The key above a claim ("Vendor") |
| Data | JetBrains Mono | 11–12 | 11 | Addresses, facts, counts, evidence |
| Caps label | JetBrains Mono, uppercase, tracking 0.14em, `dim` | 10.5 | 10 | "COVERAGE", "MAP", "SELECTED" |
| Brand | JetBrains Mono, tracking 0.3em | 12 | 12 | "CARTA" |
| Map label | Inter Regular | 12 | 11 | Beside each device |

Sizes are in px for the desktop and the web, and sp on Android.

**Rules**

- **Measured values are always monospace.** Monospace reads as data.
- **Buttons and tabs are sentence case.** Uppercase belongs to the brand, caps labels and tags.
- **Text follows the reader's text size** on every surface, and layouts are checked at Android's
  largest font scale (2.0).

### 2.3 Spacing and shape

- Spacing steps: 4, 8, 12, 16, 22, 28, 34. Columns and panels are padded 28 on the desktop and 22
  on the phone.
- **Shape:** hairlines and open space separate everything; there are almost no boxes.
  - The **Survey pill** is fully rounded, 1 px `fg` stroke, filled `fg` on hover or press.
  - The **Stop pill** is the same in `red`.
  - The **phone sheet** has 18 px top corners, the only large rounded surface.
  - The **`STOPPED EARLY` tag** is a 1 px `warn` box with square corners.
  - **Findings** have no box; a 3 px rule on the left in `red`, `warn` or `faint` by severity.
  - **Confidence words** have no box on the desktop and phone; the word itself is the badge.

### 2.4 Motion

- The map lays out once, by a short fixed force simulation seeded on a circle, then stops. The
  same survey always gives the same picture.
- The phone's sheet eases to its height in 0.22 s and follows the finger exactly while dragged.
- While a survey runs, the map dims to 45%. Nothing else animates.

---

## 3. Layouts

### 3.1 Desktop and wide browser (mock D)

```
 CARTA   Survey   More tools later                                Open   Compare
 ─────────────────────────────────────────────────────────────────────────────────
 14 devices             │ MAP          click a device, drag…  │ Device Findings³ Devices¹⁴ Drift³
 on 192.168.10.0/24     │                                     │ ─────────────────────────────
 from     192.168.10.20 │            the map                  │ SELECTED
 started  23 Nov, 15:56 │                                     │ gateway.lab.example
 took     41 s          │                                     │ 192.168.10.1   00:00:5e:00:53:01
 gateway  192.168.10.1  │                                     │
 COVERAGE               │                                     │ Name
 ● ● ○ ● … (254 dots)   │                                     │ gateway.lab.example   inferred
 14 answered 240 silent │                                     │ rdns: PTR gateway.lab.example.
 0 unreached            │                                     │ …
 Survey complete        │ ● identified ○ partly known         │
 ───────────────        │ ◌ unidentified                 Fit  │
 ( Survey ) quietly     │                                     │
```

- Three columns, 340 / flexible / 380, separated by `hair` lines.
- **Left: what this survey is.** The headline, four facts, the coverage matrix and counts, and at
  the bottom the status, a 2 px progress line and the controls.
- **Centre: the map.** Caps label and hint at the top, legend at the bottom left, **Fit** at the
  bottom right.
- **Right: one panel, four tabs.**

### 3.2 Phone and narrow browser (mock E)

- **At rest:** top bar (CARTA, Compare); the headline, one line of facts, the coverage matrix and
  counts; the map; the sheet peeking with its tabs and the selected device's name; the bottom bar.
- **Sheet:** peek, half and full. The grip drags it, or a tap steps through the three. A tab tapped
  at peek opens it to half. Opening it collapses the headline to one line and hides the facts and
  matrix, so the map keeps room. The map pans just enough to keep the selected device above the
  sheet.
- **Bottom bar:** the status and one sub-line on the left; the Survey pill on the right, which
  becomes Stop while running; a 2 px progress line along its top edge.
- **No "Survey quietly" on the phone.** Android cannot read the ARP cache, so a survey that sends
  nothing would find only the phone.
- **In the Android app** the web page draws the headline, matrix, map and sheet; the app draws the
  top bar and the bottom bar natively. The page knows by `?embedded`.

---

## 4. Components

| Component | Rules |
|---|---|
| **Headline** | The count is the number of devices that answered this survey. Under it, the network in `dim`. |
| **Facts** | Mono, `dim` keys and `fg` values: from, started, took, gateway. On the phone, one line. |
| **Coverage matrix** | One dot per address in the prefix, in order. Answered: `fg` filled. Asked but silent: `faint` filled. Never reached: a `dim` ring. During a survey it fills in as addresses are swept. The counts under it name all three, and a sentence says silent means unknown, not empty. A quiet survey leaves every address it did not hear as never reached, because it never asked. |
| **Status** | Plain words: "Survey complete", "Sweeping 108 of 254", "Stopping…", "Stopped during Sweeping" with the `STOPPED EARLY` tag, "Quiet survey: nothing was sent". |
| **Progress line** | 2 px, `fg` over `hair`. Hidden when idle without moving anything. |
| **Survey pill** | The one strong control. Becomes **Stop** in `red` while running. |
| **Survey quietly** | Plain `dim` text beside the pill (desktop and wide browser only). |
| **Map device** | Identified: filled `fg`. Partly known: `fg` ring. Unidentified: dashed `amber` ring. Infrastructure: larger. Selected: `red`, with a `red` ring around it. Label beside it in `dim`, `fg` when selected. |
| **Map label placement** | Right of the dot; if that runs off the edge or collides, left, then below; if all collide, the label shows only on hover or selection. |
| **Map gestures** | Click or tap selects; drag pans; wheel or pinch zooms; **Fit** resets. A tap target is wider than the dot. |
| **Tabs** | Text with a 1 px `fg` underline on the selected one. Counts as small mono superscripts; the Findings count is `red` when there is a risk. |
| **Device tab** | Caps "SELECTED" in `red`, the name, the addresses in mono. Then one block per claim: label, value with its confidence word on the right, evidence line under it in mono `dim`. A missing value reads "not learned" in `dim` italics. Then services. |
| **Findings tab** | One row per finding: severity rule, title, confidence word, detail in `dim`. Clicking a finding opens the device it is about. |
| **Devices tab** | A filter field (underline only), then one row per device: name left, address and kind right in mono. Clicking selects it and opens the Device tab. |
| **Drift tab** | "Since the survey of …", caveats first in `warn`, then one row per change: kind in caps, the device, what changed, and "matched by mac, observed" or "matched by address only". Clicking opens the device. |
| **Keeping a selection** | After a new survey the selected device is looked for with `drift.Find`. Found by MAC or name: it stays selected. Found by address only: it stays selected and says the match is weak. Not found: the selection clears and the panel says it did not answer this time, or was not heard by a quiet survey. Neither means it is gone. |
| **Refusal** (Android) | A title, the reason, the next step and its action, and **Show last survey** when there is one. |

---

## 5. Untrusted text

Hostnames, banners, mDNS names and page titles are whatever devices on the network chose to send.

- Web: every value goes through `esc()` before `innerHTML`, or is set with `textContent`. The page
  keeps its strict Content-Security-Policy and fetches nothing from the network.
- Desktop: values go into Fyne text objects, which never interpret markup.
- Android: the WebView has no JavaScript bridge and no file or content access, and refuses any
  request outside the bundled pages.

---

## 6. Accessibility

Contrast, measured (text needs 4.5:1, graphics that carry meaning 3:1):

| Pair | Dark | Light |
|---|---|---|
| `fg` on `bg` | 18.4 | 18.1 |
| `dim` on `bg` | 6.1 | 5.9 |
| `dim` on `panel` | 5.8 | 6.1 |
| `red` on `bg` | 5.9 | 4.6 |
| `warn` on `bg` | 9.3 | 5.2 |
| `amber` on `bg` (graphic) | 8.5 | 3.7 |
| `faint` on `bg` | 2.4 | 2.0 |
| `thread` on `bg` | 1.5 | 1.5 |
| `hair` on `bg` | 1.2 | 1.2 |

- **`faint` is never text, and never the only thing carrying a meaning.** The mocks first used it
  for evidence lines and "not learned"; those are `dim`. A silent dot is told from an answered one
  by its tone (2.4:1 against 18.4:1) and from a never-reached one by its shape.
- **`amber` is never text.** In light mode it passes only as a graphic.
- **Nothing depends on colour alone:** confidence is a word, certainty is a shape, the selection
  has a ring, the risk finding has the word in its title.
- **Keyboard:** tabs move with arrows; rows open with Enter; a skip link (web) leads past the map
  to the Devices tab; focus is visible.
- **Screen readers:** the status is a polite live region; the map has a label that names how many
  devices it shows and where the list is.
- **Touch:** every control on the phone is at least 48 × 48 dp.

---

## 7. Adding to the system

1. **A new colour** gets a dark and a light value here, its contrast in §6, and a statement of what
   it may not be used for if it fails 4.5:1. It goes into all three surfaces together: the Fyne
   theme (`desktop/shell/theme.go`), `app.css`, and Android's `colors.xml` and
   `values-night/colors.xml`.
2. **A new component** is drawn in the mocks first, then gets a row in §4.
3. **A change to the survey view** is a change to `carta ui` and the Android app together. Check
   it at 1440 and 390 wide, embedded and not, light and dark.
4. **Before shipping:** light and dark, the largest font size, both orientations on the phone, and
   a survey started, stopped and rotated through.

---

## 8. Known gaps

- Not yet checked on a physical phone: the sheet's feel, pinch beside it, haptics and TalkBack.
- The baseline list (Compare) grows without limit; there is no delete or grouping yet.
- A refusal does not clear itself when the network changes; the operator taps Survey again.
