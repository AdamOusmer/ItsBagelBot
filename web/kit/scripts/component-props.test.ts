// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { readdir, readFile } from 'node:fs/promises';
import { resolve, relative } from 'node:path';

const root = resolve(import.meta.dir, '../../..');
// Public integration contracts belong to their respective tools.
const integrations = /^(?:data-sveltekit-[\w-]+|data-astro-[\w-]+|data-cfasync|data-pagefind-[\w-]+|data-lenis-prevent|data-processed)$/;
const sources = ['ui/astro', 'ui/svelte', 'ui/lib', 'ui/styles', 'web/kit/components', 'web/kit/lib', 'web/kit/styles', ...['marketing', 'docs', 'dashboard', 'admin', 'ai'].map((app) => `web/${app}/src`)];

async function files(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const groups = await Promise.all(entries.map((entry) => {
    const path = resolve(dir, entry.name);
    if (entry.isDirectory()) return files(path);
    return /\.(?:astro|svelte|css|[cm]?[jt]s)$/.test(path) && !/\.(?:test|spec)\./.test(path) ? [path] : [];
  }));
  return groups.flat();
}

test('production components use props and refs instead of custom data attribute contracts', async () => {
  const offenders: string[] = [];
  for (const path of (await Promise.all(sources.map((dir) => files(resolve(root, dir))))).flat()) {
    const source = await readFile(path, 'utf8');
    const attributes = new Set<string>();
    for (const tag of source.matchAll(/<[A-Za-z][\w.-]*(?:\s[^<>]*?)?\/?>/g)) {
      for (const attr of tag[0].matchAll(/\b(data-[a-z][\w-]*)(?=[\s=/>])/g)) attributes.add(attr[1]);
    }
    for (const attr of source.matchAll(/\[\s*(data-[a-z][\w-]*)\b|(?:getAttribute|setAttribute|hasAttribute|removeAttribute|toggleAttribute)\(\s*['"](data-[a-z][\w-]*)['"]/g)) attributes.add(attr[1] ?? attr[2]);
    for (const attr of attributes) if (!integrations.test(attr)) offenders.push(`${relative(root, path)}: ${attr}`);
    for (const access of source.matchAll(/\.dataset(?:\.([\w]+)|\[)/g)) {
      if (access[1] !== 'lenisPrevent') offenders.push(`${relative(root, path)}: ${access[0]}`);
    }
  }
  expect(offenders).toEqual([]);
});
