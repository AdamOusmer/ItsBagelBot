// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createServer as createHttpServer } from 'node:http';
import { createServer as createHttpsServer } from 'node:https';
import { existsSync, readFileSync, watch } from 'node:fs';
import { createSecureContext } from 'node:tls';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import sirv from 'sirv';

const build = path.resolve(process.env.SVELTEKIT_BUILD_DIR || 'build');
const { handler } = await import(pathToFileURL(path.join(build, 'handler.js')).href);

const port = Number(process.env.PORT || 3000);
const host = process.env.HOST || '0.0.0.0';
const socketPath = process.env.SOCKET_PATH;
const shutdownTimeoutSeconds = Number(process.env.SHUTDOWN_TIMEOUT || 30);

const immutable = '/_app/immutable/';

const revalidated = new Set(['/service-worker.js', '/offline.html']);

// SWR only works when the edge TTL is CDN-Cache-Control: any s-maxage makes Cloudflare drop stale-while-revalidate.
function setStaticHeaders(res, pathname) {
  if (pathname.includes(immutable)) {
    res.setHeader('cache-control', 'public,max-age=31536000,immutable');
  } else if (pathname === '/_app/version.json') {
    // Deploy-detection poll target: browsers must revalidate or stale tabs never notice a deploy.
    res.setHeader('cache-control', 'no-cache');
    res.setHeader('cdn-cache-control', 'max-age=30, stale-while-revalidate=30');
  } else if (revalidated.has(pathname)) {
    res.setHeader('cache-control', 'no-cache');
  } else if (!pathname.startsWith('/_app/')) {
    res.setHeader('cache-control', 'public, max-age=86400');
    res.setHeader('cdn-cache-control', 'max-age=604800, stale-while-revalidate=86400, stale-if-error=86400');
  }
}

const client = sirv(path.join(build, 'client'), {
  etag: true,
  gzip: true,
  brotli: true,
  setHeaders: setStaticHeaders
});

// no-store: the CF edge would replay a cached mid-deploy miss to every client.
function send404(req, res) {
  res.statusCode = 404;
  res.setHeader('Cache-Control', 'no-store');
  const url = req.url || '';
  if (url.endsWith('.css')) res.setHeader('Content-Type', 'text/css');
  else if (url.endsWith('.js')) res.setHeader('Content-Type', 'application/javascript');
  else res.setHeader('Content-Type', 'text/plain; charset=utf-8');
  res.end('Not found');
}

const prerenderedDir = path.join(build, 'prerendered');
const prerendered = existsSync(prerenderedDir)
  ? sirv(prerenderedDir, {
      etag: true,
      gzip: true,
      brotli: true
    })
  : undefined;

function sendLive(res) {
  res.statusCode = 200;
  res.setHeader('Cache-Control', 'no-store');
  res.setHeader('Content-Type', 'text/plain; charset=utf-8');
  res.end('ok');
}

const requestListener = (req, res) => {
  if (req.url === '/healthz') {
    sendLive(res);
    return;
  }
  client(req, res, () => {
    const next = () => {
      // Missing /_app assets must 404: the SSR fallback would answer a .js URL with HTML.
      if (req.url && req.url.startsWith('/_app/')) {
        send404(req, res);
        return;
      }

      handler(req, res, () => send404(req, res));
    };

    if (prerendered) {
      prerendered(req, res, next);
    } else {
      next();
    }
  });
};

const tlsCert = process.env.TLS_CERT_FILE;
const tlsKey = process.env.TLS_KEY_FILE;
const tlsOptions = () => ({ cert: readFileSync(tlsCert), key: readFileSync(tlsKey) });
const tlsEnabled = Boolean(tlsCert && tlsKey);

function startServer() {
  const instance = tlsEnabled ? createHttpsServer(tlsOptions(), requestListener) : createHttpServer(requestListener);
  // Must outlast the proxy's idle timeout (Traefik and Go default 90 s), or it reuses a socket the pod just closed: 502.
  instance.keepAliveTimeout = 95_000;
  instance.headersTimeout = 96_000;
  const onListening = () => {
    console.log(socketPath ? `Listening on ${socketPath}` : `Listening on ${tlsEnabled ? 'https' : 'http'}://${host}:${port}`);
  };
  instance.listen(socketPath ? { path: socketPath } : { host, port }, onListening);
  return instance;
}

let server = startServer();

// Bun 1.4 has no setSecureContext and ignores SNICallback, so a renewed cert needs a fresh listener.
function reloadTls() {
  try {
    createSecureContext(tlsOptions());
  } catch (error) {
    console.error(`TLS reload failed; keeping the previous certificate: ${error}`);
    return;
  }
  const previous = server;
  previous.close();
  previous.closeIdleConnections?.();
  setTimeout(() => previous.closeAllConnections?.(), shutdownTimeoutSeconds * 1000).unref();
  server = startServer();
  console.log('Reloaded TLS certificate');
}

if (tlsEnabled) {
  let timer;
  // Kubelet renews a secret volume by swapping the ..data symlink, so watch the directory, not the file.
  for (const dir of new Set([path.dirname(tlsCert), path.dirname(tlsKey)])) {
    watch(dir, () => {
      clearTimeout(timer);
      timer = setTimeout(reloadTls, 1000);
    }).unref();
  }
}

function shutdown(signal) {
  console.log(`Received ${signal}; closing HTTP server`);
  server.close(() => {
    process.exit(0);
  });
  server.closeIdleConnections?.();
  setTimeout(() => {
    server.closeAllConnections?.();
    process.exit(1);
  }, shutdownTimeoutSeconds * 1000).unref();
}

process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);
