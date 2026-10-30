// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { hexToHsv, hsvToHex, parseHex } from '../lib/color';
import ColorPicker from '../svelte/ColorPicker.svelte';
import { svelteHtml } from './contract';

describe('color', () => {
  test('parseHex accepts 3 or 6 digits with or without the hash', () => {
    expect(parseHex('#62A593')).toBe('#62a593');
    expect(parseHex('62a593')).toBe('#62a593');
    expect(parseHex(' #fA0 ')).toBe('#ffaa00');
  });

  test('parseHex rejects anything else', () => {
    for (const input of ['', '#12345', '#gggggg', 'red', '#1234567', null, undefined]) expect(parseHex(input)).toBeNull();
  });

  test('every hex survives a trip through HSV', () => {
    for (const hex of ['#000000', '#ffffff', '#62a593', '#c47a3a', '#5865f2', '#808080', '#ff0000', '#0000ff']) {
      expect(hsvToHex(hexToHsv(hex))).toBe(hex);
    }
  });

  test('hexToHsv puts pure hues on the wheel', () => {
    expect(hexToHsv('#ff0000')).toEqual({ h: 0, s: 1, v: 1 });
    expect(hexToHsv('#00ff00')).toEqual({ h: 120, s: 1, v: 1 });
    expect(hexToHsv('#0000ff')).toEqual({ h: 240, s: 1, v: 1 });
  });
});

describe('ColorPicker', () => {
  test('trigger shows the normalised hex and carries it to the form', () => {
    const html = svelteHtml(ColorPicker, { value: '#62A593', name: 'color', label: 'Reward color' });
    expect(html).toContain('<input type="hidden" name="color" value="#62a593">');
    expect(html).toContain('aria-label="Reward color"');
    expect(html).toContain('>#62A593</span>');
    expect(html).toContain('--cp-color: #62a593');
  });

  test('an unreadable value falls back to black instead of an empty swatch', () => {
    expect(svelteHtml(ColorPicker, { value: 'nope', label: 'Color' })).toContain('>#000000</span>');
  });

  test('no name means no hidden form field', () => {
    expect(svelteHtml(ColorPicker, { value: '#ffffff', label: 'Color' })).not.toContain('type="hidden"');
  });
});
