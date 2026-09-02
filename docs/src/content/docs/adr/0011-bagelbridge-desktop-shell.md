---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: "0011 - BagelBridge desktop shell"
description: "Architecture decision record: BagelBridge is a Go local agent, not Electron; a system webview (Wails) is the shell if a window is required"
---

**Date:** 2026-09-02

## Status

Proposed

## Context

BagelBridge is the process that will run on the streamer's PC: pairing with the cloud bot, OBS
(`obs-websocket`), overlay sources, alert audio, and a tray presence. The category is occupied by Electron
apps (Streamlabs Desktop, parts of StreamElements). Electron would let us drop the existing Svelte console
into a window and inherit a decade of packaging, overlay, and auto-update work.

That is the wrong cost model. The companion shares the machine with OBS and a game. Electron's empty-app
footprint is on the order of 150–300 MB on disk and 150–400 MB RSS, with a 1–3 s Chromium cold start. OBS
already embeds Chromium for Browser Sources, so shipping a second browser so that *our* HTML can render
does not buy overlay fidelity — it pays for it twice.

The rest of the system is Go services and a SvelteKit console
([ADR 0002](/adr/0002-adoption-of-go-as-primary-service-language/),
[console](/microservices/console/)). ADR 0002 already declined Rust for the fleet on learning-curve
grounds. A companion framework that introduces Node (Electron) or Rust (Tauri) as a third runtime has to
beat "a Go binary and the browser they already have."

The full comparison, including the frameworks we are not picking, lives in
[BagelBridge desktop frameworks](/architecture/bagelbridge-frameworks/).

## Decision

We will **not** ship Electron, NW.js, CEF, or any other bundled Chromium as BagelBridge.

1. **Default shape:** a signed **Go agent** in the system tray. It owns pairing, `obs-websocket`, local
   media, alert playback, and a loopback (or hosted) overlay URL. Overlay HTML renders in **OBS Browser
   Source**. Settings stay in the existing SvelteKit console.
2. **If a windowed shell is required** (onboarding wizard, alert preview, docked local UI): **Wails**
   (system webview: WebView2 / WKWebView / WebKitGTK) with the Svelte UI we already have. Prefer **Wails v2**
   while v3 is pre-GA; move to v3 when multi-window/tray API stability is the thing we need and the beta
   risk is acceptable.
3. **Overlays do not render in the companion webview.** WebView2 ≠ WKWebView ≠ OBS's Chromium. The stream
   view is the source of truth, and OBS already paid for that engine.

Tauri 2 stays on the list as the industry-default webview shell. We do not adopt it unless a later ADR
explicitly carves out Rust for this surface and supersedes the learning-curve clause in ADR 0002 for
BagelBridge only.

## Consequences

- The companion is another Go module. Shared types, `obs-websocket` clients, and signing/release scripts
  stay in the same language as `app/`. There is no Node main process and no `cargo` toolchain in the
  default path.
- Installer and idle RAM stay in the 10–30 MB class for the agent, or ~10–20 MB / 40–100 MB if we add a
  Wails window — an order of magnitude under Electron. That is the actual "faster and lighter" win.
- We give up Electron's identical-everywhere Chromium, `electron-builder` maturity, and the easy
  click-through always-on-top overlay-over-the-game pattern. If that product requirement appears, we
  re-open this ADR; the next experiment is Wails/Tauri transparency, not a Chromium bundle.
- Linux is the awkward webview (WebKitGTK packages). Windows (WebView2, already on Win10/11) and macOS
  (WKWebView) are the real streamer platforms.
- Wails v3's multi-window story is the one we want and is still beta as of 2026. Pin versions. Do not
  treat the Wails API as frozen until GA.
- A future "console as an app icon" can be a Wails or even Pake window around the existing site. That is
  not BagelBridge; it does not replace the agent.

## Alternatives considered

- **Electron.** Category default. Rejected on installer size, RSS, startup, and a Node runtime beside a Go
  fleet. Overlay Chromium already exists inside OBS.
- **Tauri 2.** Best webview framework on paper (permissions, plugins, updater, mobile). Rejected for now
  because the backend is Rust and ADR 0002 declined that learning curve for this team. Revisit if we
  accept a second systems language for the companion only.
- **Neutralino.** Smallest webview wrapper. Too thin for OBS/media/tray depth; we would grow a Go sidecar
  and then own two processes.
- **Energy (CEF mode) / CEF / NW.js.** Chromium without Electron's ecosystem. Same RAM class, worse DX.
- **Lorca / astilectron.** Chrome-must-be-installed, or Go-wrapped Electron. Unmaintained or the worst of
  both stacks.
- **Fyne / Gio / Flutter.** Native or Skia UI. Throws away the Svelte console. Acceptable only if the
  companion UI is permanently three buttons and a status line.
- **Sciter / Ultralight.** Tiny commercial HTML engines, not a full browser, overlays would still not
  match OBS. Not worth a licence eval unless we need an always-on overlay *outside* OBS.
- **Pake.** Wraps the website. Not a local bridge.
