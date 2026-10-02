// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { fitRatio, groupMin } from '../lib/text-fit';

test('fitRatio leaves text that already fits at full size', () => {
  expect(fitRatio({ needed: 200, deficit: 0 })).toBe(1);
  expect(fitRatio({ needed: 0, deficit: 0 })).toBe(1);
});

test('fitRatio scales by the missing share with a 0.99 safety margin', () => {
  expect(fitRatio({ needed: 200, deficit: 50 })).toBeCloseTo(0.7425, 10);
  expect(fitRatio({ needed: 100, deficit: 1 })).toBeCloseTo(0.9801, 10);
});

test('fitRatio never shrinks below half size', () => {
  expect(fitRatio({ needed: 200, deficit: 150 })).toBe(0.5);
  expect(fitRatio({ needed: 200, deficit: 400 })).toBe(0.5);
});

test('groupMin gives every member of a group its smallest ratio', () => {
  expect(
    groupMin([
      { group: 'hero', ratio: 1 },
      { group: undefined, ratio: 0.6 },
      { group: 'hero', ratio: 0.8 },
      { group: 'cta', ratio: 0.9 },
      { group: 'hero', ratio: 0.95 },
    ]),
  ).toEqual([0.8, 0.6, 0.8, 0.9, 0.8]);
});
