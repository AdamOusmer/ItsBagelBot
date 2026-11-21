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
  const queue = [];
  let busy = false;

  async function playNext() {
    if (busy) return;
    while (queue.length && Date.parse(queue[0].expires_at) <= Date.now()) queue.shift();
    const clip = queue[0];
    if (!clip) {
      hint.hidden = true;
      return;
    }

    busy = true;
    audio.src = '/tts/clip/' + encodeURIComponent(clip.event_id);
    try {
      await audio.play();
      if (queue[0] === clip) hint.hidden = true;
    } catch (error) {
      if (queue[0] !== clip) return;
      if (error.name === 'NotAllowedError') {
        hint.hidden = false;
        busy = false;
      } else {
        advance();
      }
    }
  }

  function advance() {
    if (!busy) return;
    queue.shift();
    busy = false;
    playNext();
  }

  audio.addEventListener('ended', advance);
  audio.addEventListener('error', () => {
    if (audio.error) advance();
  });
  hint.addEventListener('click', playNext);

  async function connect() {
    const token = location.hash.slice(1);
    if (token) {
      let session;
      try {
        session = fetch('/tts/session', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ token }),
          credentials: 'same-origin',
          redirect: 'error'
        });
      } finally {
        history.replaceState(null, '', '/tts');
      }
      if (!(await session).ok) return;
    }

    const events = new EventSource('/tts/events');
    events.onmessage = ({ data }) => {
      const clip = JSON.parse(data);
      if (typeof clip.event_id !== 'string' || !(Date.parse(clip.expires_at) > Date.now())) return;
      queue.push(clip);
      if (hint.hidden) playNext();
    };
  }

  connect().catch(() => {});
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
