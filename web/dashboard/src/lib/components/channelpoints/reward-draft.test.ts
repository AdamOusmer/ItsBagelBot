// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import type { ChannelPointReward } from '@bagel/kit';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();

const { REWARD_ERROR_KEYS, rewardErrors } = await import('./reward-draft');

const draftWithCounter = (counter: string) => ({ title: 'Hydrate', cost: 100, counter }) as ChannelPointReward;

describe('rewardErrors counter name', () => {
  test.each(['', '!', ' ! ', '  !  '])('%j errors when counter is on', (counter) => {
    expect(rewardErrors(draftWithCounter(counter), true, false).counter).toBe(REWARD_ERROR_KEYS.counter);
  });

  test('!hydrate passes', () => {
    expect(rewardErrors(draftWithCounter('!hydrate'), true, false).counter).toBeUndefined();
  });

  test('counter off skips the check', () => {
    expect(rewardErrors(draftWithCounter('!'), false, false).counter).toBeUndefined();
  });
});

describe('rewardErrors points', () => {
  const draftWithPoints = (points: number) => ({ title: 'Hydrate', cost: 100, counter: '', points }) as ChannelPointReward;

  test.each([
    { name: 'zero', points: 0 },
    { name: 'NaN from a cleared input', points: Number.NaN },
    { name: 'a fraction', points: 1.5 },
    { name: 'above the server cap', points: 100_000_001 }
  ])('$name errors when points are on', ({ points }) => {
    expect(rewardErrors(draftWithPoints(points), false, true).points).toBe(REWARD_ERROR_KEYS.points);
  });

  test.each([1, 100_000_000])('%d passes', (points) => {
    expect(rewardErrors(draftWithPoints(points), false, true).points).toBeUndefined();
  });

  test('points off skips the check', () => {
    expect(rewardErrors(draftWithPoints(0), false, false).points).toBeUndefined();
  });
});
