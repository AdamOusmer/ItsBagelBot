// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync } from 'node:fs';
import { mkdtemp, mkdir, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import { pruneServerChunks } from './prune-server-chunks.mjs';

describe('pruneServerChunks', () => {
  test('keeps chunks an entry reaches, statically or dynamically, and removes the rest', async () => {
    const root = await mkdtemp(join(tmpdir(), 'prune-server-chunks-'));
    await mkdir(join(root, 'chunks'));
    await mkdir(join(root, 'entries'));
    await mkdir(join(root, '.vite'));
    await writeFile(join(root, '.vite', 'manifest.json'), '{}');
    await writeFile(join(root, 'index.js'), 'import{a}from"./chunks/a.js";export{a};');
    await writeFile(join(root, 'entries', 'page.js'), 'export const load=()=>import("../chunks/b.js");');
    await writeFile(join(root, 'chunks', 'a.js'), 'import{c}from"./c.js";export const a=c;');
    await writeFile(join(root, 'chunks', 'b.js'), 'export const b=1;');
    await writeFile(join(root, 'chunks', 'c.js'), 'export const c=1;');
    await writeFile(join(root, 'chunks', 'demo-data.js'), 'import{c}from"./c.js";export const d=c;');

    const removed = pruneServerChunks(root);

    expect(removed).toEqual([join(root, 'chunks', 'demo-data.js')]);
    expect(['a', 'b', 'c'].map((name) => existsSync(join(root, 'chunks', `${name}.js`)))).toEqual([true, true, true]);
    expect(existsSync(join(root, '.vite'))).toBe(false);
  });
});
