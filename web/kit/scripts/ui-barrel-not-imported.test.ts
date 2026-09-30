// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readdir, readFile } from 'node:fs/promises';
import { join, relative, resolve } from 'node:path';

const webRoot = resolve(import.meta.dir, '../..');

const SKIPPED_DIRS = new Set(['node_modules', '.svelte-kit', '.astro', 'dist', 'build', '.output']);
const SCANNED = ['.svelte', '.astro', '.ts', '.js', '.mjs'];
const SELF = relative(webRoot, import.meta.path);

const BARREL = /(?:from\s*|import\s*\(\s*|import\s+)(['"])@bagel\/ui\/(svelte|astro)\1/g;

const HINT =
  'Import the component or module directly (`@bagel/ui/svelte/Button.svelte`, `@bagel/ui/svelte/toast`, ' +
  '`@bagel/ui/lib/select`). The barrel evaluates every component and its stylesheet, so a route that ' +
  'names one control ships the whole library.';

export function barrelImports(source: string): number[] {
  return [...source.matchAll(BARREL)].map((match) => source.slice(0, match.index).split('\n').length);
}

async function* sources(dir: string): AsyncGenerator<string> {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!SKIPPED_DIRS.has(entry.name)) yield* sources(full);
    } else if (SCANNED.some((ext) => entry.name.endsWith(ext))) yield full;
  }
}

describe('barrelImports', () => {
  test('finds a static and a dynamic barrel import', () => {
    const source = "import { Button } from '@bagel/ui/svelte';\nconst m = await import('@bagel/ui/astro');\n";
    expect(barrelImports(source)).toEqual([1, 2]);
  });

  test('finds a multi-line named import', () => {
    expect(barrelImports("import {\n  Button,\n  Text,\n} from '@bagel/ui/svelte';\n")).toEqual([4]);
  });

  test('ignores a direct component or module path', () => {
    const source = "import Button from '@bagel/ui/svelte/Button.svelte';\nimport { toast } from '@bagel/ui/svelte/toast';\n";
    expect(barrelImports(source)).toEqual([]);
  });
});

describe('apps import @bagel/ui components directly', () => {
  test('no file under web imports the svelte or astro barrel', async () => {
    const offenders: string[] = [];
    for await (const file of sources(webRoot)) {
      const rel = relative(webRoot, file);
      if (rel === SELF) continue;
      for (const line of barrelImports(await readFile(file, 'utf8'))) offenders.push(`${rel}:${line}`);
    }
    expect(offenders, `${HINT}\n${offenders.join('\n')}`).toEqual([]);
  });
});
