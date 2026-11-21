// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import {
  DEFAULT_EVERY_MINUTES, DEFAULT_POWERED_ALIGN, DEFAULT_SHOW_SECONDS, MAX_EVERY_MINUTES, MAX_SHOW_SECONDS,
  MIN_EVERY_MINUTES, MIN_SHOW_SECONDS, POWERED_ALIGNS, POWERED_DIRECTIONS, POWERED_PAGE_URL,
  defaultPoweredConfig, poweredConfigError, poweredPreviewUrl, type PoweredConfig
} from './overlays';

const source = await Bun.file(new URL('../../../internal/domain/rpc/overlays/overlays.go', import.meta.url)).text();

const goInt = (name: string) => Number(source.match(new RegExp(`^\\s*${name}\\s*=\\s*(\\d+)$`, 'm'))?.[1]);
const goValues = (type: string) => [...source.matchAll(new RegExp(`^\\s*\\w+\\s+${type}\\s*=\\s*"([a-z]+)"$`, 'gm'))].map((m) => m[1]);

test('powered overlay limits match the Go contract', () => {
  expect({
    MinEveryMinutes: MIN_EVERY_MINUTES,
    MaxEveryMinutes: MAX_EVERY_MINUTES,
    DefaultEveryMinutes: DEFAULT_EVERY_MINUTES,
    MinShowSeconds: MIN_SHOW_SECONDS,
    MaxShowSeconds: MAX_SHOW_SECONDS,
    DefaultShowSeconds: DEFAULT_SHOW_SECONDS,
    Direction: POWERED_DIRECTIONS,
    Align: POWERED_ALIGNS,
    DefaultAlign: DEFAULT_POWERED_ALIGN,
    PageURL: POWERED_PAGE_URL
  }).toEqual({
    MinEveryMinutes: goInt('MinEveryMinutes'),
    MaxEveryMinutes: goInt('MaxEveryMinutes'),
    DefaultEveryMinutes: goInt('DefaultEveryMinutes'),
    MinShowSeconds: goInt('MinShowSeconds'),
    MaxShowSeconds: goInt('MaxShowSeconds'),
    DefaultShowSeconds: goInt('DefaultShowSeconds'),
    Direction: goValues('Direction'),
    Align: goValues('Align'),
    DefaultAlign: source.match(/Align:\s+Align(\w+),/)?.[1]?.toLowerCase(),
    PageURL: source.match(/PoweredURLPrefix = "([^"#]+)#"/)?.[1]
  });
});

const base = defaultPoweredConfig();

test.each([
  { name: 'the default', config: base, error: null },
  { name: 'the shortest wait', config: { ...base, every_minutes: 5 }, error: null },
  { name: 'a wait under the floor', config: { ...base, every_minutes: 4 }, error: 'overlays.invalidEvery' },
  { name: 'a wait over the cap', config: { ...base, every_minutes: 121 }, error: 'overlays.invalidEvery' },
  { name: 'a fractional wait', config: { ...base, every_minutes: 7.5 }, error: 'overlays.invalidEvery' },
  { name: 'the longest show', config: { ...base, show_seconds: 20 }, error: null },
  { name: 'a show under the floor', config: { ...base, show_seconds: 5 }, error: 'overlays.invalidShow' },
  { name: 'no edges', config: { ...base, directions: [] }, error: 'overlays.invalidFrom' },
  { name: 'a repeated edge', config: { ...base, directions: ['top', 'top'] }, error: 'overlays.invalidFrom' },
  { name: 'an unknown edge', config: { ...base, directions: ['middle'] }, error: 'overlays.invalidFrom' },
  { name: 'an unknown position', config: { ...base, align: 'middle' }, error: 'overlays.invalidAlign' }
])('validates $name', ({ config, error }) => {
  expect(poweredConfigError(config as PoweredConfig)).toBe(error as never);
});

test('the preview link carries the look and the plan, never a token', () => {
  const url = new URL(poweredPreviewUrl({ ...base, show_seconds: 12, directions: ['left', 'bottom'], align: 'center' }, true));
  expect([url.origin + url.pathname, Object.fromEntries(url.searchParams), url.hash]).toEqual([
    POWERED_PAGE_URL, { preview: '1', show: '12', from: 'left,bottom', align: 'center', premium: '1' }, ''
  ]);
});
