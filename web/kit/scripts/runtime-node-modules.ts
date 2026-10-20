#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync } from 'node:fs';
import { cp, readFile } from 'node:fs/promises';
import { dirname, join, relative, resolve } from 'node:path';

async function dependencyNames(packageDir: string): Promise<string[]> {
  const manifest = JSON.parse(await readFile(join(packageDir, 'package.json'), 'utf8'));
  return Object.keys(manifest.dependencies ?? {});
}

function resolvePackage(name: string, fromDir: string, root: string): string {
  for (let dir = fromDir; ; dir = dirname(dir)) {
    const candidate = join(dir, 'node_modules', name);
    if (existsSync(join(candidate, 'package.json'))) return candidate;
    if (dir === root) throw new Error(`${name} is not installed (required from ${relative(root, fromDir) || '.'})`);
  }
}

export async function runtimePackageDirs(modulesDir: string, roots: string[]): Promise<string[]> {
  const root = dirname(modulesDir);
  const seen = new Set<string>();
  const pending = roots.map((name) => resolvePackage(name, root, root));
  while (pending.length) {
    const dir = pending.pop()!;
    if (seen.has(dir)) continue;
    seen.add(dir);
    for (const name of await dependencyNames(dir)) pending.push(resolvePackage(name, dir, root));
  }
  return [...seen].sort();
}

const BUILD_ONLY_FILE = /\.(map|d\.[cm]?ts)$/;

export async function copyRuntimeNodeModules(modulesDir: string, outDir: string, roots: string[]): Promise<number> {
  const dirs = await runtimePackageDirs(modulesDir, roots);
  for (const dir of dirs) {
    const nested = join(dir, 'node_modules');
    await cp(dir, join(outDir, relative(modulesDir, dir)), {
      recursive: true,
      verbatimSymlinks: true,
      filter: (src) => src !== nested && !BUILD_ONLY_FILE.test(src)
    });
  }
  return dirs.length;
}

if (import.meta.main) {
  const [modulesDir, outDir, ...roots] = process.argv.slice(2);
  if (!modulesDir || !outDir || roots.length === 0) {
    console.error('usage: runtime-node-modules.ts <node_modules> <out> <package>...');
    process.exit(2);
  }
  const count = await copyRuntimeNodeModules(resolve(modulesDir), resolve(outDir), roots);
  console.log(`Copied ${count} runtime packages to ${outDir}`);
}
