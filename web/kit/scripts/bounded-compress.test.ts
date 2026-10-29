// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync } from 'node:fs';
import { mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { brotliDecompressSync, gunzipSync } from 'node:zlib';
import { describe, expect, test } from 'bun:test';
import { boundedCompress } from './bounded-compress.mjs';

async function fixture(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), 'bounded-compress-'));
  await mkdir(join(root, 'i18n', 'fr'), { recursive: true });
  await writeFile(join(root, 'app.js'), 'export const a = 1;'.repeat(50));
  await writeFile(join(root, 'i18n', 'fr', 'slice.json'), JSON.stringify({ hello: 'bonjour' }));
  await writeFile(join(root, 'logo.png'), 'not text');
  return root;
}

describe('boundedCompress', () => {
  test('writes gzip and brotli siblings that decode to the source, for text assets only', async () => {
    const root = await fixture();
    await boundedCompress(root, 1);

    for (const file of [join(root, 'app.js'), join(root, 'i18n', 'fr', 'slice.json')]) {
      const source = await readFile(file);
      expect(gunzipSync(await readFile(`${file}.gz`))).toEqual(source);
      expect(brotliDecompressSync(await readFile(`${file}.br`))).toEqual(source);
    }
    expect([existsSync(join(root, 'logo.png.gz')), existsSync(join(root, 'logo.png.br'))]).toEqual([false, false]);
  });

  test('a missing directory is a no-op', async () => {
    await expect(boundedCompress(join(tmpdir(), 'bounded-compress-missing'))).resolves.toEqual([]);
  });
});
