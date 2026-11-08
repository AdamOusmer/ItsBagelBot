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

function markupAttributes(source: string): string[] {
  const tags = source.matchAll(/<[A-Za-z][\w.-]*(?:\s[^<>]*?)?\/?>/g);
  return Array.from(tags).flatMap(([tag]) =>
    Array.from(tag.matchAll(/\b(data-[a-z][\w-]*)(?=[\s=/>])/g), (attr) => attr[1]));
}

function domAttributes(source: string): string[] {
  const accesses = source.matchAll(/\[\s*(data-[a-z][\w-]*)\b|(?:getAttribute|setAttribute|hasAttribute|removeAttribute|toggleAttribute)\(\s*['"](data-[a-z][\w-]*)['"]/g);
  return Array.from(accesses, (attr) => attr[1] ?? attr[2]);
}

function customContracts(source: string): string[] {
  const attributes = new Set([...markupAttributes(source), ...domAttributes(source)]);
  return Array.from(attributes).filter((attribute) => !integrations.test(attribute));
}

function customDatasetAccesses(source: string): string[] {
  return Array.from(source.matchAll(/\.dataset(?:\.([\w]+)|\[)/g))
    .filter((access) => access[1] !== 'lenisPrevent')
    .map((access) => access[0]);
}

async function violations(path: string): Promise<string[]> {
  const source = await readFile(path, 'utf8');
  return [...customContracts(source), ...customDatasetAccesses(source)]
    .map((contract) => `${relative(root, path)}: ${contract}`);
}

test('production components use props and refs instead of custom data attribute contracts', async () => {
  const paths = (await Promise.all(sources.map((dir) => files(resolve(root, dir))))).flat();
  const offenders = (await Promise.all(paths.map(violations))).flat();
  expect(offenders).toEqual([]);
});
