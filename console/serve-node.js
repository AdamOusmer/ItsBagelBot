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
    if (pathname.startsWith(immutable)) {
      res.setHeader('cache-control', 'public,max-age=31536000,immutable');
    }
  }
});

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
      handler(req, res, () => {
        res.statusCode = 404;
        const url = req.url || '';
        if (url.endsWith('.css')) res.setHeader('Content-Type', 'text/css');
        else if (url.endsWith('.js')) res.setHeader('Content-Type', 'application/javascript');
        else res.setHeader('Content-Type', 'text/plain; charset=utf-8');
        res.end('Not found');
      });
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
