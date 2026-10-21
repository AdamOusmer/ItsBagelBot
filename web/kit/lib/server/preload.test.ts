// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { preloadStrategy } from './preload';

const font = (path: string) => preloadStrategy({ type: 'font', path });

describe('preloadStrategy', () => {
  test('preloads only the fonts that block first paint', () => {
    expect(font('/_app/immutable/assets/syne-latin.BVsvWWA4.woff2')).toBe(true);
    expect(font('/_app/immutable/assets/dm-sans-latin.Xz1IZZA0.woff2')).toBe(true);
    expect(font('/_app/immutable/assets/dm-mono-400.4GdczIuU.woff2')).toBe(true);
    expect(font('/_app/immutable/assets/dm-sans-latin-italic.Cz4n9dED.woff2')).toBe(false);
    expect(font('/_app/immutable/assets/dm-mono-500.DRMDZjhP.woff2')).toBe(false);
    expect(font('/_app/immutable/assets/caveat-latin.C1hSzPvX.woff2')).toBe(false);
  });

  test('keeps script and stylesheet preloads', () => {
    expect(preloadStrategy({ type: 'js', path: '/_app/immutable/chunks/a.js' })).toBe(true);
    expect(preloadStrategy({ type: 'css', path: '/_app/immutable/assets/a.css' })).toBe(true);
    expect(preloadStrategy({ type: 'asset', path: '/_app/immutable/assets/a.png' })).toBe(false);
  });
});
