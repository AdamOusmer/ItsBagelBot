// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { GIVEAWAY_LIMITS } from './giveaway';

const root = join(import.meta.dir, '../../../..');
const draw = readFileSync(join(root, 'app/twitch/sesame/engine/draw/policy.go'), 'utf8');
const rules = readFileSync(join(root, 'app/twitch/sesame/engine/eligibility/policy.go'), 'utf8');
const wire = readFileSync(join(root, 'internal/domain/rpc/draw/giveaway.go'), 'utf8');
const modules = readFileSync(join(root, 'internal/domain/rpc/modules/giveaway.go'), 'utf8');

const units: Record<string, number> = { Second: 1, Minute: 60, Hour: 3600 };

test.each(Object.entries(GIVEAWAY_LIMITS))('%s matches Sesame or its RPC contract', (key, limit) => {
  const name = key[0].toUpperCase() + key.slice(1);
  const source = key === 'maxWinners' ? wire : draw + rules + modules;
  const match = source.match(new RegExp(
    `\\b${name}\\s*[:=]\\s*(true|false|[\\d_]+|time\\.Second)(?: \\* time\\.(\\w+))?`
  ));
  const raw = match?.[1];
  const value = raw === 'time.Second' ? 1 : Number(raw?.replaceAll('_', '')) * (units[match?.[2] ?? ''] ?? 1);
  expect(typeof limit === 'boolean' ? raw : value).toBe(typeof limit === 'boolean' ? String(limit) : limit);
});
