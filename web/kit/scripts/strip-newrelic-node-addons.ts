#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * Strip New Relic's optional Node/V8 native addons from a hoisted node_modules.
 *
 * bun + those .node files is not "bun cannot run the JS agent". newrelic's
 * optionalDependencies (@newrelic/fn-inspect, @newrelic/native-metrics,
 * @datadog/pprof) are V8 addons. Distroless bun dlopens a present .node and
 * aborts the process (`undefined symbol GetScriptOrigin`) — that is not a
 * catchable MODULE_NOT_FOUND. Measured 2026-09-15 on the v0.2.2-beta
 * console-dashboard image: the JS agent listened, then the first public
 * request 502'd dashboard and stats. native-metrics already failed closed
 * (no libstdc++). NEW_RELIC_CODE_LEVEL_METRICS_ENABLED=false was not enough
 * because bun loads the addon on require(), before the agent's try/catch.
 *
 * Deleting the packages makes require() fail closed; the JS agent stays up
 * (startSegment/noticeError, node:http). Do not set [install] optional=false
 * in bunfig.toml: vite/rolldown/lightningcss ship platform bindings as
 * optionalDependencies and `bun run build` needs those. Runtime stage has
 * no shell, so the console Containerfiles run this after the production
 * install.
 */
import { readdir, rm } from 'node:fs/promises';
import { join, sep } from 'node:path';

export const NEW_RELIC_NODE_ADDON_DIRS = [
  ['@newrelic', 'fn-inspect'],
  ['@newrelic', 'native-metrics'],
  ['@datadog', 'pprof']
] as const;

function isAddonDir(parent: string, name: string): boolean {
  return NEW_RELIC_NODE_ADDON_DIRS.some(([scope, pkg]) => parent === scope && name === pkg);
}

function isAddonNode(path: string): boolean {
  if (!path.endsWith('.node')) return false;
  return (
    path.includes(`${sep}@newrelic${sep}`) || path.includes(`${sep}@datadog${sep}pprof${sep}`)
  );
}

async function pathsUnder(dir: string, dirs: boolean): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true }).catch((err: NodeJS.ErrnoException) => {
    if (err.code === 'ENOENT') return [];
    throw err;
  });
  const nested = await Promise.all(
    entries.map((entry) => {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        return pathsUnder(path, dirs).then((below) => (dirs ? [path, ...below] : below));
      }
      return Promise.resolve(dirs ? [] : [path]);
    })
  );
  return nested.flat();
}

/** Remove scoped addon directories anywhere under `root` (hoisted or nested). */
export async function stripNewRelicNodeAddons(root: string): Promise<void> {
  const targets = (await pathsUnder(root, true)).filter((dir) => {
    const parts = dir.split(sep);
    const name = parts.at(-1);
    const parent = parts.at(-2);
    return Boolean(name && parent && isAddonDir(parent, name));
  });
  // Deepest first so a nested copy is gone before its parent walk continues.
  targets.sort((a, b) => b.length - a.length);
  await Promise.all(targets.map((dir) => rm(dir, { recursive: true, force: true })));
}

export async function leftoverNewRelicNodeAddons(root: string): Promise<string[]> {
  return (await pathsUnder(root, false)).filter(isAddonNode);
}

if (import.meta.main) {
  const root = process.argv[2] ?? 'node_modules';
  await stripNewRelicNodeAddons(root);
  const leftover = await leftoverNewRelicNodeAddons(root);
  if (leftover.length) {
    console.error(leftover.join('\n'));
    process.exit(1);
  }
}
