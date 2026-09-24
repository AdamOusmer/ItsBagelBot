// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createServer as createHttpServer } from 'node:http';
import { createServer as createHttpsServer } from 'node:https';
import { existsSync, readFileSync } from 'node:fs';
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

const client = sirv(path.join(build, 'client'), {
  etag: true,
  gzip: true,
  brotli: true,
  setHeaders: (res, pathname) => {
    if (pathname.includes(immutable)) {
      res.setHeader('cache-control', 'public,max-age=31536000,immutable');
    } else if (pathname === '/_app/version.json') {
      // Deploy-detection poll target: if cached, stale tabs never notice a deploy.
      res.setHeader('cache-control', 'no-store');
    }
  }
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

const requestListener = (req, res) => {
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
const server =
  tlsCert && tlsKey
    ? createHttpsServer({ cert: readFileSync(tlsCert), key: readFileSync(tlsKey) }, requestListener)
    : createHttpServer(requestListener);

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

if (socketPath) {
  server.listen({ path: socketPath }, () => {
    console.log(`Listening on ${socketPath}`);
  });
} else {
  server.listen({ host, port }, () => {
    console.log(`Listening on ${tlsCert && tlsKey ? 'https' : 'http'}://${host}:${port}`);
  });
}
