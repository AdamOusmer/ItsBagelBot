// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import { leftoverNewRelicNodeAddons, stripNewRelicNodeAddons } from './strip-newrelic-node-addons';

async function fixture(): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), 'nr-addons-'));
  const hoisted = join(root, '@newrelic', 'fn-inspect', 'prebuilds');
  const nested = join(root, 'newrelic', 'node_modules', '@newrelic', 'native-metrics');
  const pprof = join(root, '@datadog', 'pprof');
  const keep = join(root, 'sirv');
  await mkdir(hoisted, { recursive: true });
  await mkdir(nested, { recursive: true });
  await mkdir(pprof, { recursive: true });
  await mkdir(keep, { recursive: true });
  await writeFile(join(hoisted, 'linux-x64.node'), 'v8');
  await writeFile(join(nested, 'metrics.node'), 'v8');
  await writeFile(join(pprof, 'pprof.node'), 'v8');
  await writeFile(join(keep, 'index.js'), 'ok');
  return root;
}

describe('stripNewRelicNodeAddons', () => {
  test('removes hoisted and nested V8 addons and leaves the rest', async () => {
    const root = await fixture();
    try {
      expect(await leftoverNewRelicNodeAddons(root)).toHaveLength(3);
      await stripNewRelicNodeAddons(root);
      expect(await leftoverNewRelicNodeAddons(root)).toEqual([]);
      expect(await Bun.file(join(root, 'sirv', 'index.js')).text()).toBe('ok');
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
