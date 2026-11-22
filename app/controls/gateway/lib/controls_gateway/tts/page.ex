# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Page do
  @css """
  html, body { margin: 0; background: transparent; }
  button { margin: 8px; padding: 6px 10px; border: 0; border-radius: 8px;
    background: #222; color: white; font: 14px system-ui, sans-serif; cursor: pointer; }
  """

  @js """
  const audio = document.querySelector('audio');
  const hint = document.querySelector('button');
  const inObs = Boolean(window.obsstudio);
  const token = location.hash.slice(1);
  const queue = [];
  const seen = new Set();
  let active = true;
  let visible = true;
  let watchdog = null;
  let playbackStartedAt = 0;
  let playbackBudget = 0;
  let retryCap = 2000;
  let lastEventId = '';

  if (token) history.replaceState(null, '', '/tts');

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

  function dropExpired() {
    const firstWaiting = watchdog === null ? 0 : 1;
    const now = Date.now();
    while (queue[firstWaiting]?.expiresAt <= now) queue.splice(firstWaiting, 1);
  }

  function playNext() {
    dropExpired();
    if (watchdog !== null || !sourceReady()) return;
    const clip = queue[0];
    if (!clip) {
      clearAudio();
      return;
    }
    startClip(clip);
  }

  async function startClip(clip) {
    const src = '/tts/clip/' + encodeURIComponent(clip.event_id);
    if (audio.getAttribute('src') !== src) {
      audio.src = src;
      playbackBudget = clip.duration_ms + 5000;
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
        advance();
        return;
      }
      resetWatchdog();
      hint.hidden = false;
    }
  }

  function advance() {
    if (watchdog === null) return;
    clearAudio();
    queue.shift();
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
    if (audio.error) advance();
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

  function waitToRetry(retryAfter = 0) {
    const delay = Math.max(retryAfter, retryCap * (0.5 + Math.random() * 0.5));
    retryCap = Math.min(60000, retryCap * 2);
    return new Promise((resolve) => setTimeout(resolve, delay));
  }

  function retryAfterMs(response) {
    if (response?.status !== 429) return 0;
    const raw = response.headers.get('retry-after');
    const seconds = Number(raw);
    const delay = Number.isFinite(seconds) ? seconds * 1000 : Date.parse(raw) - Date.now();
    return Math.max(0, delay || 0);
  }

  async function requestUntil(path, options, stopStatus) {
    for (;;) {
      const response = await fetch(path, { ...options, redirect: 'error' }).catch(() => null);
      const status = response?.status ?? 0;
      if (response?.ok || status === stopStatus) return status;
      await waitToRetry(retryAfterMs(response));
    }
  }

  function receiveClip({ data, lastEventId: sequence }) {
    lastEventId = sequence;
    const clip = JSON.parse(data);
    clip.expiresAt = Date.parse(clip.expires_at);
    const expired = !(clip.expiresAt > Date.now());
    if (expired || typeof clip.event_id !== 'string' || seen.has(clip.event_id)) return;
    seen.add(clip.event_id);
    if (seen.size > 100) seen.delete(seen.values().next().value);
    queue.push(clip);
    if (hint.hidden) playNext();
    else dropExpired();
  }

  function stopPlayback() {
    queue.length = 0;
    clearAudio();
  }

  async function recover() {
    await waitToRetry();
    const status = await requestUntil('/tts/events', { method: 'HEAD' }, token ? 401 : undefined);
    if (status === 204) {
      stopPlayback();
      return;
    }
    await connect();
  }

  async function exchangeToken() {
    if (!token) return true;
    const status = await requestUntil('/tts/session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token })
    }, 404);
    return status !== 404;
  }

  async function connect() {
    if (!(await exchangeToken())) {
      stopPlayback();
      return;
    }

    const path = lastEventId ? '/tts/events?after=' + encodeURIComponent(lastEventId) : '/tts/events';
    const events = new EventSource(path);
    events.onopen = () => { retryCap = 2000; };
    events.onmessage = receiveClip;
    events.onerror = () => {
      if (events.readyState === EventSource.CLOSED) recover();
    };
  }

  connect();
  """

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

  def reply(req, headers) do
    headers =
      Map.merge(headers, %{
        "content-type" => "text/html; charset=utf-8",
        "content-security-policy" => @csp,
        "x-content-type-options" => "nosniff"
      })

    {:ok, :cowboy_req.reply(200, headers, @html, req), nil}
  end
end
