// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';

import { placeDropdown } from '../lib/dropdown-placement';

const VIEWPORT = { width: 1280, height: 800 };
const content = (height: number) => () => height;

describe('placeDropdown', () => {
  test('opens under the field, flush with its left edge and at its width', () => {
    const field = { left: 293, top: 382, bottom: 418, width: 434 };
    expect(placeDropdown(field, VIEWPORT, 380, content(189))).toEqual({ left: 293, width: 434, top: 422, maxHeight: 370 });
  });

  test('a short list stays under the field even when there is more room above', () => {
    const field = { left: 973, top: 564, bottom: 600, width: 260 };
    expect(placeDropdown(field, VIEWPORT, 380, content(90))).toEqual({ left: 973, width: 260, top: 604, maxHeight: 188 });
  });

  test('a list that does not fit below opens above, anchored by its bottom edge to the field', () => {
    const field = { left: 990, top: 564, bottom: 600, width: 243 };
    expect(placeDropdown(field, VIEWPORT, 380, content(3500))).toEqual({ left: 990, width: 243, bottom: 240, maxHeight: 380 });
  });

  test('when neither side fits, the roomier side wins and caps the height', () => {
    const short = { width: 1280, height: 400 };
    expect(placeDropdown({ left: 100, top: 150, bottom: 186, width: 300 }, short, 380, content(1000)))
      .toEqual({ left: 100, width: 300, top: 190, maxHeight: 202 });
    expect(placeDropdown({ left: 100, top: 250, bottom: 286, width: 300 }, short, 380, content(1000)))
      .toEqual({ left: 100, width: 300, bottom: 154, maxHeight: 238 });
  });

  test('narrow fields get a readable minimum width and the panel stays inside the viewport', () => {
    expect(placeDropdown({ left: 1200, top: 100, bottom: 136, width: 60 }, VIEWPORT, 380, content(100)))
      .toEqual({ left: 1072, width: 200, top: 140, maxHeight: 380 });
    expect(placeDropdown({ left: 0, top: 100, bottom: 136, width: 900 }, { width: 600, height: 800 }, 380, content(100)))
      .toEqual({ left: 8, width: 584, top: 140, maxHeight: 380 });
  });

  test('measures the content at the width it will be shown at', () => {
    const widths: number[] = [];
    placeDropdown({ left: 10, top: 10, bottom: 46, width: 150 }, VIEWPORT, 380, (width) => {
      widths.push(width);
      return 100;
    });
    expect(widths).toEqual([200]);
  });
});
