import { createServer } from 'node:http';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import sirv from 'sirv';

const build = path.resolve(process.env.SVELTEKIT_BUILD_DIR || 'build');
const { handler } = await import(pathToFileURL(path.join(build, 'handler.js')).href);

const port = Number(process.env.PORT || 3000);
const host = process.env.HOST || '0.0.0.0';
const socketPath = process.env.SOCKET_PATH;
const shutdownTimeout = Number(process.env.SHUTDOWN_TIMEOUT || 30);

const immutable = '/_app/immutable/';

const client = sirv(path.join(build, 'client'), {
  etag: true,
  gzip: true,
  brotli: true,
  setHeaders: (res, pathname) => {
    if (pathname.includes(immutable)) {
      // Content-hashed names; safe to cache hard at the browser and CF edge.
      res.setHeader('cache-control', 'public,max-age=31536000,immutable');
    } else if (pathname === '/_app/version.json') {
      // Deploy-detection poll target. If CF or the browser ever cached it, the
      // client's `updated` store would never flip on a new deploy and stale tabs
      // would keep fetching deleted chunks. Must never be cached.
      res.setHeader('cache-control', 'no-store');
    }
  }
});

// A miss must never be cached. A transient 404 (a chunk requested mid-deploy,
// before this pod rolled, or routed to a peer that hasn't) would otherwise be
// memoized by the CF edge and replayed to every client = a 404 storm. no-store
// keeps the miss to the one request so the client's version-poll reload recovers.
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

const server = createServer((req, res) => {
  client(req, res, () => {
    const next = () => {
      // A static asset under /_app that sirv could not find: it does not exist on
      // this pod. Don't fall through to the SSR handler (which would 200 an HTML
      // shell for a .js URL and break the SPA); return a non-cacheable 404.
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
});

function shutdown(signal) {
  console.log(`Received ${signal}; closing HTTP server`);
  server.close(() => {
    process.exit(0);
  });
  server.closeIdleConnections?.();
  setTimeout(() => {
    server.closeAllConnections?.();
    process.exit(1);
  }, shutdownTimeout * 1000).unref();
}

process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);

if (socketPath) {
  server.listen({ path: socketPath }, () => {
    console.log(`Listening on ${socketPath}`);
  });
} else {
  server.listen({ host, port }, () => {
    console.log(`Listening on http://${host}:${port}`);
  });
}
