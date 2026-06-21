import { createServer } from 'node:http';
import { existsSync, readdirSync } from 'node:fs';
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

// SvelteKit's single-bundle output hashes can diverge between the ARM and Intel
// builds despite BUILD_VERSION pinning, causing 404s when Traefik routes a request
// to a different arch than the one that served the HTML. We dynamically find the
// local bundle names and rewrite mismatched requests to them.
let localBundleJs;
let localBundleCss;
try {
  const immutableDir = path.join(build, 'client', '_app', 'immutable');
  if (existsSync(immutableDir)) {
    const files = readdirSync(immutableDir);
    const jsFile = files.find(f => f.startsWith('bundle.') && f.endsWith('.js'));
    if (jsFile) localBundleJs = `/_app/immutable/${jsFile}`;
    
    const assetsDir = path.join(immutableDir, 'assets');
    if (existsSync(assetsDir)) {
      const assetFiles = readdirSync(assetsDir);
      const cssFile = assetFiles.find(f => f.startsWith('bundle.') && f.endsWith('.css'));
      if (cssFile) localBundleCss = `/_app/immutable/assets/${cssFile}`;
    }
  }
} catch (e) {
  console.warn('Failed to resolve local bundle hashes:', e);
}

const client = sirv(path.join(build, 'client'), {
  etag: true,
  gzip: true,
  brotli: true,
  setHeaders: (res, pathname) => {
    if (pathname.includes(immutable)) {
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
  if (req.url) {
    const urlPath = req.url.split('?')[0];
    if (localBundleJs && urlPath.startsWith('/_app/immutable/bundle.') && urlPath.endsWith('.js')) {
      req.url = localBundleJs;
    } else if (localBundleCss && urlPath.startsWith('/_app/immutable/assets/bundle.') && urlPath.endsWith('.css')) {
      req.url = localBundleCss;
    }
  }

  client(req, res, () => {
    const next = () => {
      if (req.url && req.url.startsWith('/_app/')) {
        res.statusCode = 404;
        const url = req.url || '';
        if (url.endsWith('.css')) res.setHeader('Content-Type', 'text/css');
        else if (url.endsWith('.js')) res.setHeader('Content-Type', 'application/javascript');
        else res.setHeader('Content-Type', 'text/plain; charset=utf-8');
        res.end('Not found');
        return;
      }

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
