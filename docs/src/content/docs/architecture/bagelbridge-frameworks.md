---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: BagelBridge desktop frameworks
description: Electron-class options for the local companion, ranked by weight, speed, and fit with the Go + Svelte stack.
---

BagelBridge is the local companion on the streamer's PC: pair with the cloud bot, talk to OBS, host overlay
sources, play alerts, sit in the tray. Streamlabs Desktop, StreamElements.Live, and Streamer.bot are the
shape of the product, not the shape of the stack. Those products pay the **Electron tax** (a private Chromium
plus Node) so a web UI can look identical everywhere. A stream PC already runs OBS, a game, and Chrome. A
second Chromium is the thing we are trying not to ship.

This page is the comparison. The proposed decision that follows from it is
[ADR 0011](/adr/0011-bagelbridge-desktop-shell/). Numbers are **typical empty-app / small-app order of
magnitude as of 2026**, not BagelBridge measurements. Real RSS is dominated by whatever the UI actually
renders.

## What the companion actually has to do

| Job | Where it should run | Why |
|---|---|---|
| Overlay widgets, alerts, goal bars | **OBS Browser Source** (or equivalent) | OBS already embeds Chromium. Duplicating that engine in our process wastes RAM the game needs. |
| Pairing, tray status, local media, OBS scene control, alert tester | A **small native process** on the PC | Needs filesystem, tray, `obs-websocket`, maybe a loopback HTTP server. Does not need a browser. |
| Settings, commands, billing | The existing **SvelteKit console** in the user's browser | Already shipped. Wrapping it in a desktop window does not make it faster. |

That split is the real alternative to Electron. Most "desktop Twitch tools" conflate three products into one
window. We should not, unless a later product decision says the companion *must* be a docked dashboard.

Constraints inherited from the rest of the system:

- Go is the service language ([ADR 0002](/adr/0002-adoption-of-go-as-primary-service-language/)). Rust was
  weighed there and declined on learning-curve grounds.
- The console UI is Svelte. Reusing it inside a webview is free; rewriting it in Dart, QML, or Fyne widgets
  is not.
- Streamers are Windows-first (x86 and ARM), with a minority on macOS (Apple Silicon) and Linux. The
  companion shares the machine with OBS + a game, so idle RAM and CPU are product requirements, not
  polish.

## Scorecard

| Framework | Backend | Renderer | Installer | Idle RAM | Cold start | Reuses Svelte? | Fit |
|---|---|---|---|---|---|---|---|
| **Thin Go agent** (no window) | Go | none (OBS Chromium for overlays) | 5–15 MB | 10–30 MB | milliseconds | n/a (console stays in the browser) | **Default** |
| **Wails v2** | Go | system webview | 10–20 MB | 40–100 MB | < 0.5 s | yes | Best Electron-shaped shell |
| **Wails v3** (beta) | Go | system webview | 10–20 MB | 40–100 MB | < 0.5 s | yes | Same, plus multi-window; still beta |
| **Tauri 2** | Rust | system webview | 5–15 MB | 30–80 MB | ~0.5 s | yes | Best-in-class webview shell; new language |
| **Neutralino** | JS + small native core | system webview | 2–5 MB | 30–70 MB | ~0.4 s | yes | Tiny wrapper, weak native depth |
| **Energy (webview mode)** | Go | system webview | 10–30 MB | 40–100 MB | < 1 s | yes | More control, smaller community |
| **Fyne / Gio** | Go | own widgets | 10–25 MB | 30–80 MB | fast | **no** | Native Go UI; throw away the console |
| **Flutter Desktop** | Dart | Skia | 30–50 MB | 100–150 MB | ~0.7 s | **no** | Pixel-perfect, new stack |
| **Pake** | Rust | system webview | ~10 MB | 40–80 MB | ~0.5 s | wraps the website | Website-in-a-window, not a bridge |
| **Lorca** | Go | installed Chrome | ~5 MB + Chrome | a Chrome process | Chrome's | yes | Unmaintained; needs Chrome; no window control |
| **Energy (CEF) / CEF** | Go / C++ | bundled Chromium | 80–150 MB | Electron-like | Electron-like | yes | Chromium without Electron's ecosystem |
| **NW.js** | Node | bundled Chromium | 150–300 MB | 150–400 MB | 1–3 s | yes | Electron with a smaller community |
| **Electron** | Node | bundled Chromium | 150–300 MB | 150–400 MB | 1–3 s | yes | Baseline we are trying to beat |
| **Sciter / Ultralight** | C++ | custom HTML | 5–20 MB | 30–80 MB | fast | HTML subset | Commercial licence, not full CSS/JS |

System webview means **WebView2** on Windows (Chromium/Edge, usually already installed), **WKWebView** on
macOS, **WebKitGTK** on Linux. That is why Tauri/Wails/Neutralino installers are an order of magnitude
smaller than Electron: they do not ship a browser.

## Camp 0 — no desktop window (lightest)

### Thin Go agent + OBS Browser Source + existing console

A signed Go binary in the tray. It pairs with the cloud bot, serves overlay URLs on localhost (or points OBS
at the hosted overlay pages), speaks `obs-websocket`, plays alert sounds, watches a media folder. The
dashboard stays in the browser the user already has open.

- **For:** Smallest RAM/CPU. One language with the rest of the fleet. Overlays render in the Chromium OBS
  already paid for, so they match what the stream actually shows. No webview version skew. Matches
  [ADR 0002](/adr/0002-adoption-of-go-as-primary-service-language/) and the
  [native-binary resource model](/infrastructure/hardware-and-cluster/).
- **Against:** No docked "app" chrome. First-run UX is a tray icon and a browser tab, not a Streamlabs-style
  window. Click-through desktop overlays (widgets floating over the game, *outside* OBS) are out of scope
  unless we add a window later.

This is the thing that is "like Electron but way better, faster, and lighter": it is not Electron at all,
and it does not pretend to be.

## Camp 1 — system webview (Electron-shaped, ~10× lighter)

Use this camp only if BagelBridge must own a real window (onboarding wizard, alert preview, docked
dashboard, always-on-top tester).

Shared tradeoffs for the whole camp:

- **For:** HTML/CSS/Svelte unchanged. Installer ~10–20 MB. Idle RAM a fraction of Electron. Startup under a
  second. OS-updated webview (security patches without us shipping Chromium).
- **Against:** Windows / macOS / Linux webviews are not the same engine. Linux WebKitGTK is the weak
  platform. Codec / WebGL / CSS edge cases will differ from OBS's Chromium, so **overlays still should not
  live in this window**. Auto-update and code-signing exist but are younger than `electron-builder`.

### Wails v2 (stable) / Wails v3 (beta)

Go backend, system webview, first-class Svelte templates, in-memory bindings (no localhost server in
production). v3 (beta through 2026) adds multi-window, a cleaner app API, and experimental mobile. v2
remains the stable line.

- **For:** Same language as every data service. `go build` is the release. Fastest path to a tray + window
  without introducing Rust. Bindings from Svelte to Go are generated, which is the IPC we would otherwise
  hand-roll.
- **Against:** Smaller ecosystem than Tauri. v3 multi-window/tray is what a companion actually wants, and
  v3 is still pre-GA — pin a version, expect API churn. Linux depends on WebKitGTK being present.

**This is the Electron analogue that fits this repo**, if a shell is required.

### Tauri 2

Rust backend, system webview, capability-based permissions, iOS/Android from v2, the current default
answer to "not Electron" in the industry. Cap, Spacedrive, Mockoon, and a pile of overlay experiments use
it. Typical empty app: ~10 MB on disk, ~30–80 MB RSS.

- **For:** Strongest webview security model (deny-by-default commands). Best plugin story (fs, store,
  updater, websocket, window state). Deepest production track record in this camp. Mobile if we ever want
  a phone companion from the same shell.
- **Against:** Backend is Rust. [ADR 0002](/adr/0002-adoption-of-go-as-primary-service-language/) declined
  Rust for the fleet on learning-curve grounds; using it here means a second systems language on a
  one-person team, plus a `cargo` toolchain in CI next to Go. IPC is `invoke` to Rust commands, not Go
  methods, so local OBS/media logic cannot be shared packages with `app/` without a FFI boundary.

Pick Tauri only if we deliberately supersede ADR 0002 *for this one surface*. Do not pick it because it is
the blog-default Electron killer.

### Neutralino

JS/TS talking to a tiny native helper. Smallest installer in the webview camp (2–5 MB). No Go, no Rust.

- **For:** Fastest "wrap this Svelte page in a window". Fine for a settings popover.
- **Against:** Native API surface is thin (files, tray, window). A bridge that speaks OBS, plays audio, and
  watches folders will outgrow it and grow a Go sidecar anyway — two processes, two update channels. Security
  model is not Tauri's. Treat as a utility framework, not an application platform.

### Energy (webview mode)

Go bindings over WebView2 / WebKit, optional CEF. More window-level control than Wails; much smaller
community; CGO-or-binary-helper story to keep track of.

- **For:** Stay in Go; can switch to CEF later if webview drift becomes a support nightmare.
- **Against:** You are the framework. Packaging, updater, and "how do I do a tray on macOS" become our
  problems. Only worth it if Wails blocks on a windowing feature we cannot patch around.

### Pake

Turns a URL into a desktop window (Tauri under the hood). Fine for "the console, but as an app icon".
Not a bridge: no OBS, no local media, no tray agent of our own.

## Camp 2 — bundled Chromium (the thing we are leaving)

### Electron

Node main process + Chromium renderer. Streamlabs Desktop, Discord, Slack, VS Code. Mature
packaging, auto-update, native modules, `BrowserWindow` transparency / `setIgnoreMouseEvents` for
click-through overlays, identical rendering on every OS.

- **For:** Every streaming-tool edge case is already solved. Overlay HTML inside the app matches overlay
  HTML inside OBS (both Chromium). Huge hiring/ecosystem pool.
- **Against:** 150–300 MB installer, 200–400 MB idle RSS before our UI exists, 1–3 s cold start, a Node
  main process next to a Go fleet. On a 16 GB streaming PC this is the difference between "the game
  hitching" and not. Security is historically the RCE poster child when `nodeIntegration` is mishandled.
  We would maintain Chromium version bumps forever.

Electron is the product-shape reference, not a candidate.

### NW.js

Same Chromium+Node idea, weaker ecosystem. No reason to pick this over Electron if we were already
paying the tax.

### CEF and Energy (CEF mode)

Chromium without Node. Still ships a browser. Same RAM class as Electron, worse DX. The only honest
reason is "we need Chromium-identical rendering *inside our process*." Overlays should be in OBS
instead, which already is Chromium.

### Lorca / astilectron

Lorca drives an *installed* Chrome over the DevTools protocol. Tiny binary, no window chrome control,
effectively unmaintained, fails if Chrome is missing. Astilectron wraps Electron from Go — both
languages' costs, neither's benefits. Both are traps.

## Camp 3 — native widgets (not Electron-like)

### Fyne / Gio

Pure Go UI, own renderer, small binaries. No HTML.

- **For:** One language, no webview skew, real native window.
- **Against:** We already have a Svelte console. A Fyne BagelBridge cannot share components, design
  tokens, or overlay HTML with it. Two UIs to keep in sync on a one-person team.

Use only if the companion UI is *tiny* (pair + status + three buttons) and we are sure it will stay tiny.

### Flutter Desktop

Skia, pixel-perfect, one codebase with a future mobile app. Dart is a new language, no Svelte reuse,
system tray/menu integration is still the weak part. Memory sits closer to a light Electron than to
Wails. Not the "lighter than Electron" win we want.

### Qt / Slint / Iced / Avalonia / MAUI

Wrong talent pool (C++, Rust UI DSLs, C#). Excellent engines, irrelevant here unless the team changes.

### Sciter / Ultralight

Tiny custom HTML engines, commercial licences, subset of the web. Antivirus vendors love Sciter. We
would rewrite overlay HTML against a non-browser and still not match OBS. Skip unless a licence +
engine eval later proves a 5 MB always-on overlay *outside* OBS is a product requirement.

## Ranking for this repo

1. **Thin Go agent** — default. Fastest, lightest, same language, overlays in OBS.
2. **Wails v2 now, v3 when GA (or earlier if multi-window is blocking)** — if we need a real window.
   Reuses Svelte. Stays in Go.
3. **Tauri 2** — only with an explicit ADR that carves out Rust for the companion.
4. **Energy webview / Neutralino** — fallbacks if Wails blocks; Neutralino only with a Go sidecar.
5. **Fyne** — only for a tray-sized UI we will never grow into a dashboard.
6. **Electron / CEF / NW.js** — rejected for the companion. Chromium belongs in OBS, not in our
   installer.

## What we would still need Electron for

A click-through, always-on-top, HTML overlay *over the game*, outside OBS, with Chromium-identical
CSS. That is a Streamlabs Desktop feature. It is also the most expensive RAM feature on a gaming PC,
and OBS Browser Source already covers the "what the stream sees" case. If that product requirement
lands, re-open this page — the answer is still more likely Tauri/Wails with a transparency hack than
a 200 MB Electron.

## See also

- [ADR 0011 — BagelBridge desktop shell](/adr/0011-bagelbridge-desktop-shell/)
- [ADR 0002 — Go as primary service language](/adr/0002-adoption-of-go-as-primary-service-language/)
- [Console](/microservices/console/)
- [System overview](/architecture/)
