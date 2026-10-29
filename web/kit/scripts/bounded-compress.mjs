// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createReadStream, createWriteStream, existsSync, readdirSync, statSync } from 'node:fs';
import { extname, join } from 'node:path';
import { pipeline } from 'node:stream/promises';
import zlib from 'node:zlib';

const EXTENSIONS = new Set(['.html', '.js', '.mjs', '.json', '.css', '.svg', '.xml', '.wasm', '.txt', '.md', '.mdx']);

// SvelteKit's builder.compress opens every file at once; the image build's 1024-descriptor limit fails it with EMFILE.
export const COMPRESS_CONCURRENCY = 32;

/** @param {string} dir @returns {string[]} */
function listFiles(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    return entry.isDirectory() ? listFiles(path) : [path];
  });
}

/** @param {string} file @param {'gz' | 'br'} format */
function compressFile(file, format) {
  const encoder =
    format === 'br'
      ? zlib.createBrotliCompress({
          params: {
            [zlib.constants.BROTLI_PARAM_MODE]: zlib.constants.BROTLI_MODE_TEXT,
            [zlib.constants.BROTLI_PARAM_QUALITY]: zlib.constants.BROTLI_MAX_QUALITY,
            [zlib.constants.BROTLI_PARAM_SIZE_HINT]: statSync(file).size
          }
        })
      : zlib.createGzip({ level: zlib.constants.Z_BEST_COMPRESSION });
  return pipeline(createReadStream(file), encoder, createWriteStream(`${file}.${format}`));
}

/**
 * Writes `.gz` and `.br` siblings like SvelteKit's builder.compress, with at most `concurrency` files open.
 * @param {string} directory
 * @param {number} [concurrency]
 */
export async function boundedCompress(directory, concurrency = COMPRESS_CONCURRENCY) {
  if (!existsSync(directory)) return [];
  const files = listFiles(directory).filter((file) => EXTENSIONS.has(extname(file)));
  const jobs = files.flatMap((file) => [() => compressFile(file, 'gz'), () => compressFile(file, 'br')]);
  let next = 0;
  const worker = async () => {
    while (next < jobs.length) await jobs[next++]();
  };
  await Promise.all(Array.from({ length: Math.min(concurrency, jobs.length) }, worker));
  return files;
}

/**
 * @param {import('@sveltejs/kit').Adapter} base
 * @returns {import('@sveltejs/kit').Adapter}
 */
export function withBoundedCompress(base) {
  return { ...base, adapt: (builder) => base.adapt({ ...builder, compress: boundedCompress }) };
}
