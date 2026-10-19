// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { existsSync } from 'node:fs';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import { copyRuntimeNodeModules } from './runtime-node-modules';

async function pkg(dir: string, name: string, deps: Record<string, string> = {}): Promise<void> {
  await mkdir(dir, { recursive: true });
  await writeFile(join(dir, 'package.json'), JSON.stringify({ name, dependencies: deps }));
  await writeFile(join(dir, 'index.js'), name);
  await writeFile(join(dir, 'index.js.map'), '{}');
  await writeFile(join(dir, 'index.d.ts'), 'export {};');
}

async function fixture(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), 'runtime-modules-'));
  const modules = join(root, 'node_modules');
  await pkg(join(modules, 'app'), 'app', { shared: '2', '@scope/leaf': '1' });
  await pkg(join(modules, 'app', 'node_modules', 'shared'), 'shared@2');
  await pkg(join(modules, 'shared'), 'shared@1');
  await pkg(join(modules, '@scope', 'leaf'), '@scope/leaf', { shared: '1' });
  await pkg(join(modules, 'devtool'), 'devtool');
  return root;
}

describe('copyRuntimeNodeModules', () => {
  test('copies the dependency closure with node resolution and drops build-only files', async () => {
    const root = await fixture();
    const out = join(root, 'out');
    try {
      const count = await copyRuntimeNodeModules(join(root, 'node_modules'), out, ['app']);
      expect(count).toBe(4);
      expect(await Bun.file(join(out, 'app', 'node_modules', 'shared', 'index.js')).text()).toBe('shared@2');
      expect(await Bun.file(join(out, 'shared', 'index.js')).text()).toBe('shared@1');
      expect(await Bun.file(join(out, '@scope', 'leaf', 'index.js')).text()).toBe('@scope/leaf');
      expect(existsSync(join(out, 'devtool'))).toBe(false);
      expect(existsSync(join(out, 'app', 'index.js.map'))).toBe(false);
      expect(existsSync(join(out, 'app', 'index.d.ts'))).toBe(false);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test('fails when a dependency is not installed', async () => {
    const root = await fixture();
    try {
      await pkg(join(root, 'node_modules', 'broken'), 'broken', { missing: '1' });
      await expect(copyRuntimeNodeModules(join(root, 'node_modules'), join(root, 'out'), ['broken'])).rejects.toThrow(
        'missing is not installed'
      );
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
