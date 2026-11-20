// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { GIVEAWAY_LIMITS } from './giveaway-limits';
import { FREEZE_MAX_MINUTES } from './loyalty';

const root = join(import.meta.dir, '../../../..');
const readSource = (path: string) => readFileSync(join(root, path), 'utf8');
const draw = readSource('app/twitch/sesame/engine/draw/policy.go');
const eligibility = readSource('app/twitch/sesame/engine/eligibility/policy.go');
const drawRpc = readSource('internal/domain/rpc/draw/giveaway.go');
const moduleRpc = readSource('internal/domain/rpc/modules/giveaway.go');
const loyalty = readFileSync(process.env.LOYALTY_POLICY_GO ?? join(root, 'internal/domain/rpc/loyalty/policy.go'), 'utf8');
const giveawayPolicy = [draw, eligibility, moduleRpc].join('\n');

const units: Record<string, number> = { Second: 1, Minute: 60, Hour: 3600 };

function goValue(match: RegExpExecArray | null): number | boolean | undefined {
  if (!match) return undefined;
  const [, raw, unit] = match;
  if (raw === 'true' || raw === 'false') return raw === 'true';
  if (raw === 'time.Second') return 1;
  return Number(raw.replaceAll('_', '')) * (unit ? units[unit] : 1);
}

const pins = [
  { name: 'FreezeMaximum', web: FREEZE_MAX_MINUTES, source: loyalty, go: /^\s*FreezeMaximum\s*=\s*(\d+)\s*\*\s*time\.Minute\s*$/m },
  ...Object.entries(GIVEAWAY_LIMITS).map(([key, web]) => {
    const name = key[0].toUpperCase() + key.slice(1);
    const source = key === 'maxWinners' ? drawRpc : giveawayPolicy;
    const go = new RegExp('\\b' + name + '\\s*[:=]\\s*(true|false|[\\d_]+|time\\.Second)(?:\\s*\\*\\s*time\\.(\\w+))?(?=\\s*(?:,|$))', 'm');
    return { name, web, source, go };
  })
];

test.each(pins)('$name matches the Go policy', ({ web, source, go }) => {
  expect(goValue(go.exec(source))).toBe(web);
});
