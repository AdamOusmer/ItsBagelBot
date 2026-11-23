# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Page do
  @css """
  html, body { margin: 0; background: transparent; }
  button { margin: 8px; padding: 6px 10px; border: 0; border-radius: 8px;
    background: #222; color: white; font: 14px system-ui, sans-serif; cursor: pointer; }
  """

  @client_path Path.expand("../../../priv/overlay/client.js", __DIR__)
  @external_resource @client_path
  @client File.read!(@client_path)

  @page_js """
  const audio = document.querySelector('audio');
  const hint = document.querySelector('button');
  const inObs = Boolean(window.obsstudio);
  const token = location.hash.slice(1);
  let current = null;
  let active = true;
  let visible = true;
  let watchdog = null;
  let playbackStartedAt = 0;
  let playbackBudget = 0;

  function resetWatchdog() {
    clearTimeout(watchdog);
    watchdog = null;
  }

  function clearAudio() {
    resetWatchdog();
    hint.hidden = true;
    if (!audio.hasAttribute('src')) return;
    audio.removeAttribute('src');
    audio.load();
  }

  function sourceReady() {
    const showing = visible && !document.hidden;
    return !inObs || (active && showing);
  }

  function playNext() {
    if (watchdog !== null || !sourceReady()) return;
    if (!current) {
      clearAudio();
      return;
    }
    if (!audio.hasAttribute('src') && current.ctl.expired()) {
      skipExpired();
      return;
    }
    startClip(current);
  }

  async function startClip(item) {
    const src = '/tts/clip/' + encodeURIComponent(item.alert.event_id);
    if (audio.getAttribute('src') !== src) {
      audio.src = src;
      playbackBudget = item.alert.data.duration_ms + 5000;
    }

    const timer = setTimeout(advance, playbackBudget);
    watchdog = timer;
    playbackStartedAt = performance.now();
    try {
      await audio.play();
      if (watchdog === timer) hint.hidden = true;
    } catch (error) {
      if (watchdog !== timer) return;
      const needsGesture = !inObs && error.name === 'NotAllowedError';
      if (!needsGesture) {
        fail();
        return;
      }
      resetWatchdog();
      hint.hidden = false;
    }
  }

  function skipExpired() {
    const skipped = current;
    current = null;
    clearAudio();
    skipped.ctl.started('expired');
    skipped.ctl.done();
  }

  function advance() {
    if (watchdog === null) return;
    const finished = current;
    current = null;
    clearAudio();
    finished.ctl.started();
    finished.ctl.done();
  }

  function fail() {
    if (watchdog === null) return;
    if (current.retried) {
      advance();
      return;
    }
    current.retried = true;
    clearAudio();
    playNext();
  }

  function sourceChanged() {
    if (sourceReady()) {
      playNext();
      return;
    }
    if (watchdog === null) return;
    if (audio.ended || audio.error) {
      advance();
      return;
    }
    playbackBudget -= performance.now() - playbackStartedAt;
    resetWatchdog();
    audio.pause();
  }

  audio.addEventListener('ended', () => {
    if (audio.ended) advance();
  });
  audio.addEventListener('error', () => {
    if (audio.error) fail();
  });
  hint.addEventListener('click', playNext);

  if (inObs) {
    window.addEventListener('obsSourceActiveChanged', ({ detail }) => {
      active = detail.active;
      sourceChanged();
    });
    window.addEventListener('obsSourceVisibleChanged', ({ detail }) => {
      visible = detail.visible;
      sourceChanged();
    });
    document.addEventListener('visibilitychange', sourceChanged);
  }

  createOverlayClient({
    EventSource,
    fetch: (url, init) => fetch(url, init),
    storage: localStorage,
    now: () => Date.now(),
    setTimeout: (fn, ms) => setTimeout(fn, ms),
    clearTimeout: (id) => clearTimeout(id),
    random: () => Math.random(),
    basePath: '/tts',
    token,
    onAlert: (alert, ctl) => {
      current = { alert, ctl, retried: false };
      if (hint.hidden) playNext();
    },
    onRevision: () => {},
    onFeed: () => {},
    onStop: () => {
      current = null;
      clearAudio();
    }
  }).start();
  """

  @js @client <> @page_js

  @html """
  <!doctype html>
  <html lang="en">
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>BagelBot TTS</title>
  <style>#{@css}</style>
  <body>
  <audio preload="none"></audio>
  <button type="button" hidden>Click to enable sound</button>
  <script>#{@js}</script>
  </body>
  </html>
  """

  @script_hash Base.encode64(:crypto.hash(:sha256, @js))
  @style_hash Base.encode64(:crypto.hash(:sha256, @css))
  @csp "default-src 'none'; script-src 'sha256-#{@script_hash}'; " <>
         "style-src 'sha256-#{@style_hash}'; connect-src 'self'; media-src 'self'; " <>
         "base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

  def reply(conn, headers) do
    headers =
      Map.merge(headers, %{
        "content-type" => "text/html; charset=utf-8",
        "content-security-policy" => @csp,
        "x-content-type-options" => "nosniff"
      })

    ControlsGateway.Http.reply(conn, 200, headers, @html)
  end
end
