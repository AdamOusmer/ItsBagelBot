// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { join } from 'node:path';
import { describe, expect, test } from 'bun:test';

const WEB = join(import.meta.dir, '..', '..');

async function viteExternals(app: string): Promise<string[]> {
  const config = await Bun.file(join(WEB, app, 'vite.config.ts')).text();
  const list = config.match(/ssr:\s*{[^}]*external:\s*\[([^\]]*)\]/)?.[1] ?? '';
  return [...list.matchAll(/'([^']+)'/g)].map((m) => m[1]);
}

async function runtimeModules(app: string): Promise<string[]> {
  const containerfile = await Bun.file(join(WEB, app, 'Containerfile')).text();
  const line = containerfile.match(/runtime-node-modules\.ts node_modules \/runtime\/node_modules (.+)/)?.[1] ?? '';
  return line.trim().split(/\s+/);
}

describe('runtime node_modules', () => {
  test.each(['admin', 'dashboard'])('%s image ships every vite SSR external', async (app) => {
    const shipped = await runtimeModules(app);
    expect((await viteExternals(app)).filter((name) => !shipped.includes(name))).toEqual([]);
  });
});
