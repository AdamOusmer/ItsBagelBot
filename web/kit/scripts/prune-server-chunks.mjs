// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync, readdirSync, readFileSync, rmSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';

const SPECIFIER = /(?:\bfrom\s*|\bimport\s*\(\s*|\bimport\s*)["'](\.{1,2}\/[^"']+\.js)["']/g;

/** @param {string} dir @returns {string[]} */
function listFiles(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    return entry.isDirectory() ? listFiles(path) : [path];
  });
}

/**
 * Removes server chunks no entry reaches. The bundler emits a chunk for every dynamic import before the minifier
 * erases the dead branch that held it, so a build-time-dead demo fixture would otherwise stay in the image.
 * @param {string} serverDir
 * @returns {string[]} removed paths
 */
export function pruneServerChunks(serverDir) {
  rmSync(join(serverDir, '.vite'), { recursive: true, force: true });
  const chunksDir = join(serverDir, 'chunks');
  if (!existsSync(chunksDir)) return [];

  const files = listFiles(serverDir).filter((file) => file.endsWith('.js'));
  const chunks = new Set(files.filter((file) => file.startsWith(`${chunksDir}/`)));
  const seen = new Set();
  const pending = files.filter((file) => !chunks.has(file));
  while (pending.length) {
    const file = /** @type {string} */ (pending.pop());
    for (const [, specifier] of readFileSync(file, 'utf8').matchAll(SPECIFIER)) {
      const target = resolve(dirname(file), specifier);
      if (chunks.has(target) && !seen.has(target)) {
        seen.add(target);
        pending.push(target);
      }
    }
  }

  const removed = [...chunks].filter((chunk) => !seen.has(chunk));
  for (const chunk of removed) rmSync(chunk);
  return removed;
}

/**
 * @param {import('@sveltejs/kit').Adapter} base
 * @returns {import('@sveltejs/kit').Adapter}
 */
export function withPrunedChunks(base) {
  return {
    ...base,
    adapt: (builder) => {
      pruneServerChunks(builder.getServerDirectory());
      return base.adapt(builder);
    }
  };
}
