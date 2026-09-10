// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { applyImportCaps } from './caps';
import type { ManifestCommand } from './types';

const cmd = (n: number): ManifestCommand => ({ name: `c${n}`, responses: ['hi'] });

test('caps leave under-cap collections untouched', () => {
  const { manifest, diagnostics } = applyImportCaps({
    commands: [cmd(0)],
    timers: [{ message: 'm', interval_seconds: 60 }]
  });
  expect(manifest.commands?.length).toBe(1);
  expect(manifest.timers?.length).toBe(1);
  expect(diagnostics).toEqual([]);
});

test('caps truncate overflow and report one manifest-level warn each', () => {
  const commands = Array.from({ length: 2002 }, (_, i) => cmd(i));
  const counters = Array.from({ length: 501 }, (_, i) => ({ name: `n${i}`, value: i }));
  const { manifest, diagnostics } = applyImportCaps({ commands, counters });

  expect(manifest.commands?.length).toBe(2000);
  expect(manifest.counters?.length).toBe(500);
  expect(diagnostics.map((d) => [d.severity, d.item_index])).toEqual([
    ['warn', -1],
    ['warn', -1]
  ]);
  expect(diagnostics.map((d) => d.code)).toEqual(['manifest_commands_capped', 'manifest_counters_capped']);
});

test('fetch definitions ride the commands cap, not a number of their own', () => {
  // Under cap: carried through untouched.
  const pass = applyImportCaps({
    fetches: [{ name: 'se_weather', url: 'https://x.example/a', source: 'streamelements' }]
  });
  expect(pass.manifest.fetches).toHaveLength(1);
  expect(pass.diagnostics).toEqual([]);

  // Over the commands ceiling: truncated with one manifest-level warn. The
  // parsers already refuse synthesis past this value; this guards manifests
  // POSTed directly.
  const fetches = Array.from({ length: 2001 }, (_, i) => ({ name: `se-c${i}`, source: 'moobot' as const }));
  const { manifest, diagnostics } = applyImportCaps({ fetches });
  expect(manifest.fetches).toHaveLength(2000);
  expect(diagnostics.map((d) => d.code)).toEqual(['manifest_fetches_capped']);
});
