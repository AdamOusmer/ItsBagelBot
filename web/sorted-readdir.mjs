// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-nocheck: deliberate monkey-patch of the fs overloads; typing it buys nothing.
import fs from 'node:fs';

const readdirSync = fs.readdirSync;

fs.readdirSync = function deterministicRouteRead(directory, options) {
  const entries = readdirSync.call(this, directory, options);
  const path = String(directory).replaceAll('\\', '/');

  if (!path.includes('/src/')) return entries;

  return entries.sort((a, b) => {
    const aName = typeof a === 'string' || Buffer.isBuffer(a) ? a : a.name;
    const bName = typeof b === 'string' || Buffer.isBuffer(b) ? b : b.name;
    return Buffer.from(aName).compare(Buffer.from(bName));
  });
};
