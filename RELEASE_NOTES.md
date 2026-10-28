# Carta — Release Notes

A network survey instrument that documents what it cannot see: a CLI for any laptop, and an
Android app.

---

## Unreleased

Rename this heading to the version number when tagging (`docs/RELEASING.md`).

**New: Carta for Android (prototype).**

- Surveys the Wi-Fi or Ethernet network the phone is on, with the same Go core as the CLI. The
  result is shown in the same UI as `carta ui`, laid out for a phone.
- It won't survey over a mobile carrier's network, or while a VPN is on. In both cases it can't
  be sure which network its probes would reach.
- Each survey records what a phone can't see. Android hides the ARP cache and other devices'
  hardware addresses, so the survey has no layer-2 evidence and no vendor lookups, and says so in
  its coverage and its findings.
- **Compare** shows what changed since an earlier survey that you choose. Carta can't tell two
  networks apart reliably, so it doesn't guess. It warns when the gateway, DHCP server, DNS
  servers or instrument differ, and marks changes matched only by address as weak.
- Surveys stay on the phone. They are excluded from backup and from device-to-device transfer.

**The app's design, rebuilt on Material 3.**

- **One typeface for everything measured: JetBrains Mono,** bundled rather than borrowed from the
  phone. `carta ui` uses it too.
- **The Survey button is at the bottom,** within thumb reach, and stays at full width. On a phone
  on its side the actions move to the single top row instead, so the map keeps its height.
- **The status strip only describes the survey:** its stage and count while running, the result
  when done, and a `STOPPED EARLY` tag when it was cut short. Stop now shows **Stopping…** until
  the survey has wound down.
- **A refusal is a screen of its own,** saying what is wrong and what to do, with a button that
  opens VPN or Wi-Fi settings and a way back to the last survey.
- **Compare opens a sheet.** Each earlier survey shows its network fingerprint in mono, and any
  field that differs from the survey on screen is marked. Picking one opens straight on the
  Drift tab, and a snackbar gives the summary.
- **The map fills the screen,** with the details panel sliding over it. Drag or tap its handle to
  peek, half or full. Back lowers it before leaving the app. The legend is behind a button on the
  map.
- **Easier to use without looking, or without a mouse:**
  - one haptic pulse when a survey finishes and a different one on a refusal;
  - a "Skip the map" link and a spoken summary of the map for screen readers;
  - keyboard-navigable tabs and device list;
  - map labels that grow with the system text size.

**Changed in the core (CLI too).**

- **A survey that is cut short now says so.** Before, Ctrl-C or the deadline produced a survey
  that reported every address as swept. Now only addresses actually reached count, and the
  survey records which stage it stopped in. The map draws the unreached part of the range as
  open space, distinct from the hatching for addresses that didn't answer.
- **Drift checks more than the prefix.** Two surveys on `192.168.1.0/24` behind different
  gateways are no longer treated as the same network. Each change says how it was matched (MAC,
  hostname or address) and how much that match is worth.
- **Reverse DNS falls back to the next DNS server** when one doesn't answer, instead of trying
  only the first.
- **The UI** works with touch: pan, pinch-zoom and larger tap targets. In a narrow window the map
  fills it and the details slide up from the bottom. A Drift tab appears when there is a
  comparison to show. The page runs under a strict Content-Security-Policy and uses the bundled
  JetBrains Mono, which adds about 200 KB to the binary. On the desktop the layout is unchanged,
  except that the coverage strip now sits under the map rather than over it.
- **The SQLite writer is its own package** (`internal/store/sqlitedb`), so builds that don't need
  the database don't link the SQLite engine.
