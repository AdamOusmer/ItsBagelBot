// SvelteKit assigns client node IDs in the order returned by readdirSync while
// walking src/routes. Overlay filesystems can return a different order on the
// native ARM64 and x86_64 image builds, which makes their SSR HTML and client
// bundles incompatible even when they come from the same commit.
const fs = require('node:fs');

const readdirSync = fs.readdirSync;

fs.readdirSync = function deterministicRouteRead(directory, options) {
  const entries = readdirSync.call(this, directory, options);
  const path = String(directory).replaceAll('\\', '/');

  if (!path.includes('/src/routes')) return entries;

  return entries.sort((a, b) => {
    const aName = typeof a === 'string' || Buffer.isBuffer(a) ? a : a.name;
    const bName = typeof b === 'string' || Buffer.isBuffer(b) ? b : b.name;
    return Buffer.from(aName).compare(Buffer.from(bName));
  });
};
