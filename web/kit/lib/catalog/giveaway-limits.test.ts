// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { GIVEAWAY_LIMITS } from './giveaway-limits';

test('giveaway eligibility limits match Sesame', () => {
  const policy = readFileSync(join(import.meta.dir, '../../../../app/twitch/sesame/engine/eligibility/policy.go'), 'utf8');
  for (const [key, limit] of Object.entries(GIVEAWAY_LIMITS)) {
    const goName = key[0].toUpperCase() + key.slice(1);
    const value = policy.match(new RegExp(`\\b${goName}: ([\\d_]+)`))?.[1];
    expect(Number(value?.replaceAll('_', ''))).toBe(limit);
  }
});
