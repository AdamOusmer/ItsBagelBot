// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import type { OverlayWidget } from '../lib/overlay-document';
import { parseWidget } from './settings';

const widget = (kind: string, settings: object | null): OverlayWidget => ({
  id: 'w1', name: 'Widget', kind: kind as OverlayWidget['kind'], x: 0, y: 0, w: 100, h: 100, rotation: 0, opacity: 1,
  hidden: false, locked: false, settings
});

describe('parseWidget', () => {
  test('fills defaults for missing settings', () => {
    expect(parseWidget(widget('image', {})).settings).toEqual({ media: '', fit: 'contain' });
  });

  test('keeps asset references and custom values', () => {
    const custom = parseWidget(widget('custom', { bundle: 'asset:a3', values: { title: 'Hi', nested: {} } }));
    expect(custom.settings).toEqual({ bundle: 'asset:a3', values: { title: 'Hi' } });
  });

  test.each([
    ['a legacy media path', 'image', { media: '/media/abc.png' }],
    ['an unknown fit', 'image', { fit: 'stretch' }],
    ['an out of range size', 'text', { style: { size: 9000 } }],
    ['a non-object settings value', 'text', 'x']
  ])('rejects %s', (_name, kind, settings) => {
    expect(() => parseWidget(widget(kind, settings as object))).toThrow();
  });
});
