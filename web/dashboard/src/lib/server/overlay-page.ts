// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHash } from 'node:crypto';

// Plain text, no interpolation: nothing a recipe or the URL carries reaches this markup.
const OVERLAY_STYLE = `html,body{margin:0;height:100%;background:transparent;overflow:hidden}
.item{position:fixed;left:50%;max-width:90vw;max-height:90vh;transform:translateX(-50%)}
.top{top:5vh}
.center{top:50%;transform:translate(-50%,-50%)}
.bottom{bottom:5vh}
.text{font:700 48px/1.2 system-ui,sans-serif;color:#fff;text-align:center;white-space:pre-wrap;overflow-wrap:anywhere;text-shadow:0 2px 8px rgba(0,0,0,.85)}`;

export const OVERLAY_SCRIPT = `(function () {
  var MAX_QUEUE = 32;
  var RETRY_MIN_MS = 2000;
  var RETRY_MAX_MS = 60000;
  var SOUND_MAX_MS = 60000;
  var stage = document.body;
  var queue = [];
  var playing = false;
  var retryMs = RETRY_MIN_MS;

  function httpsUrl(raw) {
    try {
      var url = new URL(raw);
      return url.protocol === 'https:' && url.hostname !== '' && url.username === '' && url.password === '';
    } catch (err) {
      return false;
    }
  }

  function next() {
    var item = queue.shift();
    playing = !!item;
    if (item) play(item);
  }

  function finisher(cleanup) {
    var finished = false;
    var finish = function () {
      if (finished) return;
      finished = true;
      clearTimeout(finish.timer);
      cleanup();
      next();
    };
    return finish;
  }

  function present(node, item) {
    node.className = 'item ' + (item.position === 'top' || item.position === 'bottom' ? item.position : 'center');
    var finish = finisher(function () {
      if (node.pause) node.pause();
      node.remove();
    });
    finish.timer = setTimeout(finish, item.seconds * 1000);
    node.addEventListener('error', finish);
    stage.appendChild(node);
    return finish;
  }

  function showText(item) {
    var node = document.createElement('div');
    node.textContent = item.content;
    present(node, item);
    node.className += ' text';
  }

  function showImage(item) {
    var node = document.createElement('img');
    node.alt = '';
    present(node, item);
    node.src = item.content;
  }

  function showVideo(item) {
    var node = document.createElement('video');
    node.playsInline = true;
    var finish = present(node, item);
    node.addEventListener('ended', finish);
    node.src = item.content;
    var started = node.play();
    if (started && started.catch) {
      started.catch(function () {
        node.muted = true;
        node.play().catch(finish);
      });
    }
  }

  function playSound(item) {
    var node = document.createElement('audio');
    var finish = finisher(function () {
      node.pause();
      node.removeAttribute('src');
    });
    finish.timer = setTimeout(finish, SOUND_MAX_MS);
    node.addEventListener('ended', finish);
    node.addEventListener('error', finish);
    node.volume = Math.min(100, Math.max(0, item.volume)) / 100;
    node.src = item.content;
    var started = node.play();
    if (started && started.catch) started.catch(finish);
  }

  function play(item) {
    if (item.kind === 'text') return showText(item);
    if (!httpsUrl(item.content)) return next();
    if (item.kind === 'image') return showImage(item);
    if (item.kind === 'video') return showVideo(item);
    if (item.kind === 'sound') return playSound(item);
    next();
  }

  function enqueue(item) {
    if (queue.length >= MAX_QUEUE) return;
    queue.push(item);
    if (!playing) next();
  }

  function connect() {
    var source = new EventSource(location.pathname.replace(/\\/$/, '') + '/stream');
    source.addEventListener('ready', function () {
      retryMs = RETRY_MIN_MS;
    });
    source.addEventListener('item', function (event) {
      try {
        enqueue(JSON.parse(event.data));
      } catch (err) {}
    });
    source.onerror = function () {
      source.close();
      setTimeout(connect, retryMs);
      retryMs = Math.min(retryMs * 2, RETRY_MAX_MS);
    };
  }

  connect();
})();`;

function cspHash(source: string): string {
  return `'sha256-${createHash('sha256').update(source).digest('base64')}'`;
}

export const OVERLAY_CSP = [
  "default-src 'none'",
  `script-src ${cspHash(OVERLAY_SCRIPT)}`,
  `style-src ${cspHash(OVERLAY_STYLE)}`,
  'img-src https:',
  'media-src https:',
  "connect-src 'self'",
  "base-uri 'none'",
  "form-action 'none'",
  "frame-ancestors 'none'"
].join('; ');

const OVERLAY_HTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="robots" content="noindex, nofollow">
<title>Overlay</title>
<style>${OVERLAY_STYLE}</style>
</head>
<body>
<script data-cfasync="false">${OVERLAY_SCRIPT}</script>
</body>
</html>
`;

const SHARED_HEADERS = {
  'Cache-Control': 'no-store',
  'Referrer-Policy': 'no-referrer',
  'X-Content-Type-Options': 'nosniff',
  'X-Frame-Options': 'DENY',
  'X-Robots-Tag': 'noindex, nofollow',
  'Strict-Transport-Security': 'max-age=31536000; includeSubDomains'
};

export function overlayPageResponse(): Response {
  return new Response(OVERLAY_HTML, {
    headers: { ...SHARED_HEADERS, 'Content-Type': 'text/html; charset=utf-8', 'Content-Security-Policy': OVERLAY_CSP }
  });
}

function plainResponse(status: number, body: string): Response {
  return new Response(body, {
    status,
    headers: { ...SHARED_HEADERS, 'Content-Type': 'text/plain; charset=utf-8', 'Content-Security-Policy': "default-src 'none'" }
  });
}

export function overlayNotFound(): Response {
  return plainResponse(404, 'Not Found');
}

export function overlayUnavailable(): Response {
  return plainResponse(503, 'Unavailable');
}
