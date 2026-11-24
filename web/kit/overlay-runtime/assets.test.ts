// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import type { OverlayAsset } from '../lib/overlay-document';
import { OVERLAY_CDN_PREFIX } from '../lib/overlay-cdn';
import { Assets } from './assets';

const asset = (url: string): OverlayAsset => ({ type: 'png', bytes: 1, w: 1, h: 1, duration_ms: 0, sha256: 'a'.repeat(64), url });
const assets = new Assets({
  a1: asset(`${OVERLAY_CDN_PREFIX}u/7/abc`),
  evil: asset('https://example.com/u/7/abc'),
  bare: { ...asset(''), url: undefined as unknown as string }
});

test.each([
  ['a known asset resolves to its cdn url', 'asset:a1', `${OVERLAY_CDN_PREFIX}u/7/abc`],
  ['a url outside the cdn is refused', 'asset:evil', undefined],
  ['an asset without a url is refused', 'asset:bare', undefined],
  ['an unknown asset is refused', 'asset:missing', undefined],
  ['inherited keys are not assets', 'asset:constructor', undefined],
  ['a plain path is refused', '/media/abc.png', undefined],
  ['an empty reference is refused', '', undefined]
])('%s', (_name, ref, url) => {
  expect(assets.resolve(ref)?.url).toBe(url);
});
