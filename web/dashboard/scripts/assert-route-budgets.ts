// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';
process.env.NEW_RELIC_ENABLED ??= 'false';

import { groupsForRoute } from '../src/lib/catalog-groups';

const OUTPUT = fileURLToPath(new URL('../.svelte-kit/output/', import.meta.url));
const IMMUTABLE = '_app/immutable/';

interface Budget {
  js: number;
  css: number;
  catalog: number;
}

// Initial-load gzip bytes per route. A raise needs the measurement and the reason in the same commit.
const BUDGETS: Record<string, Budget> = {
  '/(public)/login': { js: 90200, css: 21700, catalog: 38800 },
  '/(app)': { js: 114400, css: 28400, catalog: 38800 },
  '/(app)/commands': { js: 182000, css: 38600, catalog: 49400 },
  '/(public)/user/[channel]': { js: 96200, css: 25000, catalog: 38800 }
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

function catalogFiles(routeId: string): string[] {
  const dir = join(OUTPUT, 'client', IMMUTABLE, 'i18n');
  const parts = ['core', ...groupsForRoute(routeId)];
  const names = readdirSync(dir);
  return parts.flatMap((part) => names.filter((name) => name.startsWith(`en.${part}.`))).map((name) => `${IMMUTABLE}i18n/${name}`);
}

async function measure(routeId: string, route: RouteEntry): Promise<Budget> {
  const js = new Set(entryFiles());
  const css = new Set<string>();
  for (const node of route.nodes) {
    const assets = await nodeAssets(node);
    assets.imports.forEach((file) => js.add(file));
    assets.stylesheets.forEach((file) => css.add(file));
  }
  return { js: sum(js), css: sum(css), catalog: sum(catalogFiles(routeId)) };
}

const routes = parseRoutes();
let failed = false;
for (const [routeId, budget] of Object.entries(BUDGETS)) {
  const route = routes.get(routeId);
  if (!route) throw new Error(`route budgets: ${routeId} is not a route`);
  const actual = await measure(routeId, route);
  for (const key of ['js', 'css', 'catalog'] as const) {
    const ok = actual[key] <= budget[key];
    failed ||= !ok;
    console.log(`${ok ? 'ok' : 'FAIL'} ${routeId} ${key}: ${actual[key]} B gzip (budget ${budget[key]})`);
  }
}
if (failed) process.exit(1);
