// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';
process.env.NEW_RELIC_ENABLED ??= 'false';


const OUTPUT = fileURLToPath(new URL('../.svelte-kit/output/', import.meta.url));
const IMMUTABLE = '_app/immutable/';

interface Budget {
  js: number;
  css: number;
  catalog: number;
  lazy: number;
}

// Initial-load gzip bytes per route. catalog is the largest non-English locale's slices for the route's initial
// chunks; lazy is the same for every slice the route can import later. A raise needs the measurement and the
// reason in the same commit.
const BUDGETS: Record<string, Budget> = {
  '/(public)/login': { js: 91600, css: 21900, catalog: 2900, lazy: 0 },
  '/(app)': { js: 118700, css: 44700, catalog: 5000, lazy: 0 },
  '/(app)/commands': { js: 138900, css: 46000, catalog: 4600, lazy: 31200 },
  '/(public)/user/[channel]': { js: 97100, css: 21500, catalog: 2800, lazy: 0 }
};

interface RouteEntry {
  nodes: number[];
}

function readText(path: string): string {
  return readFileSync(join(OUTPUT, path), 'utf8');
}

function gzipSize(path: string): number {
  return gzipSync(readFileSync(join(OUTPUT, 'client', path)), { level: 9 }).length;
}

function sum(paths: Iterable<string>): number {
  let total = 0;
  for (const path of paths) total += gzipSize(path);
  return total;
}

function parseRoutes(): Map<string, RouteEntry> {
  const manifest = readText('server/manifest-full.js');
  const nodeBlock = /nodes: \[([\s\S]*?)\n\t\t\],/.exec(manifest)?.[1] ?? '';
  const nodeFiles = [...nodeBlock.matchAll(/nodes\/(\d+)\.js/g)].map((match) => Number(match[1]));
  const routes = new Map<string, RouteEntry>();
  const pattern = /id: "([^"]+)",\s*pattern: [^,]+,\s*params: [^\n]*\n\s*page: \{ layouts: \[([^\]]*)\], errors: \[[^\]]*\], leaf: (\d+) \}/g;
  for (const match of manifest.matchAll(pattern)) {
    const layouts = match[2].split(',').map((slot) => slot.trim()).filter((slot) => /^\d+$/.test(slot));
    routes.set(match[1], { nodes: [...layouts.map(Number), Number(match[3])].map((slot) => nodeFiles[slot] ?? slot) });
  }
  if (routes.size === 0) throw new Error('route budgets: no routes found in server/manifest-full.js');
  return routes;
}

async function nodeAssets(node: number): Promise<{ imports: string[]; stylesheets: string[] }> {
  const module = await import(join(OUTPUT, `server/nodes/${node}.js`));
  return { imports: module.imports ?? [], stylesheets: module.stylesheets ?? [] };
}

function entryFiles(): string[] {
  return readdirSync(join(OUTPUT, 'client', IMMUTABLE, 'entry'))
    .filter((name) => /^(start|app)\.[^.]+\.js$/.test(name))
    .map((name) => `${IMMUTABLE}entry/${name}`);
}

function sliceLocales(): string[] {
  return readdirSync(join(OUTPUT, 'client', IMMUTABLE, 'i18n'), { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name);
}

const chunkName = (file: string): string => file.slice(file.lastIndexOf('/') + 1).replace(/\.js$/, '');

const slicedChunks = new Set<string>(JSON.parse(readText('client/_app/i18n-slices.json')));

function sliceFiles(js: Iterable<string>, locale: string): string[] {
  return [...js].filter((file) => slicedChunks.has(chunkName(file))).map((file) => `${IMMUTABLE}i18n/${locale}/${chunkName(file)}.json`);
}

function largestLocale(js: Iterable<string>): number {
  return Math.max(...sliceLocales().map((locale) => sum(sliceFiles(js, locale))));
}

interface ManifestChunk {
  file: string;
  imports?: string[];
  dynamicImports?: string[];
}

const manifest: Record<string, ManifestChunk> = JSON.parse(readText('client/.vite/manifest.json'));
const byFile = new Map(Object.values(manifest).map((chunk) => [chunk.file, chunk]));

function staticClosure(keys: readonly string[], out = new Set<string>()): Set<string> {
  for (const key of keys.filter((candidate) => manifest[candidate] && !out.has(manifest[candidate].file))) {
    out.add(manifest[key].file);
    staticClosure(manifest[key].imports ?? [], out);
  }
  return out;
}

/** Chunks a route can load after its first render through dynamic imports, other route nodes excluded. */
function lazyFiles(initial: ReadonlySet<string>): Set<string> {
  const dynamic = [...initial].flatMap((file) => byFile.get(file)?.dynamicImports ?? []).filter((key) => !key.includes('/nodes/'));
  return new Set([...staticClosure(dynamic)].filter((file) => !initial.has(file)));
}

async function measure(route: RouteEntry): Promise<Budget> {
  const js = new Set(entryFiles());
  const css = new Set<string>();
  for (const node of route.nodes) {
    const assets = await nodeAssets(node);
    assets.imports.forEach((file) => js.add(file));
    assets.stylesheets.forEach((file) => css.add(file));
  }
  return { js: sum(js), css: sum(css), catalog: largestLocale(js), lazy: largestLocale(lazyFiles(js)) };
}

const routes = parseRoutes();
let failed = false;
for (const [routeId, budget] of Object.entries(BUDGETS)) {
  const route = routes.get(routeId);
  if (!route) throw new Error(`route budgets: ${routeId} is not a route`);
  const actual = await measure(route);
  for (const key of ['js', 'css', 'catalog', 'lazy'] as const) {
    const ok = actual[key] <= budget[key];
    failed ||= !ok;
    console.log(`${ok ? 'ok' : 'FAIL'} ${routeId} ${key}: ${actual[key]} B gzip (budget ${budget[key]})`);
  }
}
if (failed) process.exit(1);
