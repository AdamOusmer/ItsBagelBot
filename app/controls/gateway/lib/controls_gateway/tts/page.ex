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

  function post(path, body) {
    return fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      redirect: 'error'
    }).catch(() => null);
  }

  const ackSettled = new Set([200, 204, 401, 404]);

  async function ackClip(handle) {
    for (let attempt = 0; attempt < 3; attempt++) {
      const response = await post('/tts/ack', { handle });
      if (ackSettled.has(response?.status)) return;
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  }

  function reportProgress() {
    const handles = queue.map((clip) => clip.handle);
    for (let start = 0; start < handles.length; start += 32) {
      post('/tts/progress', { handles: handles.slice(start, start + 32) });
    }
  }

  function playNext() {
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
        fail();
        return;
      }
      resetWatchdog();
      hint.hidden = false;
    }
  }

  function advance() {
    if (watchdog === null) return;
    clearAudio();
    ackClip(queue.shift().handle);
    playNext();
  }

  function fail() {
    if (watchdog === null) return;
    if (queue[0].retried) {
      advance();
      return;
    }
    queue[0].retried = true;
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

  setInterval(reportProgress, 15000);

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

  function receiveClip({ data }) {
    const clip = JSON.parse(data);
    if (typeof clip.event_id !== 'string' || typeof clip.handle !== 'string') return;
    if (seen.has(clip.event_id)) {
      ackClip(clip.handle);
      return;
    }
    seen.add(clip.event_id);
    if (seen.size > 100) seen.delete(seen.values().next().value);
    queue.push(clip);
    if (hint.hidden) playNext();
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

    const events = new EventSource('/tts/events');
    events.onopen = () => { retryCap = 2000; };
    events.addEventListener('clip', receiveClip);
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
