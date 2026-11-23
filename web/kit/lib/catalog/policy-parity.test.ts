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
const raffleConfig = readSource('app/twitch/sesame/engine/raffle_config.go');
const raffleCommand = readSource('app/twitch/sesame/modules/raffle.go');
const giveawayPolicy = [draw, eligibility, moduleRpc].join('\n');

const units: Record<string, number> = { Second: 1, Minute: 60, Hour: 3600 };
const goLiterals = new Map<string, boolean | string | number>([
  ['true', true],
  ['false', false],
  ['single', 'single'],
  ['multi', 'split'],
  ['time.Second', units.Second]
]);
const rafflePins: Partial<Record<keyof typeof GIVEAWAY_LIMITS, [string, RegExp]>> = {
  raffleMaxPrize: [raffleConfig, /^\s*MaxRafflePrize\s*=\s*int64\(([\d_]+)\)$/m],
  raffleDefaultPrize: [raffleConfig, /^\s*raffleDefaultPrize\s*=\s*int64\(([\d_]+)\)$/m],
  raffleMinSeconds: [raffleConfig, /^\s*minRaffleDuration\s*=\s*(\d+)\s*\*\s*time\.(Second)$/m],
  raffleDefaultSeconds: [raffleConfig, /^\s*raffleDefaultDuration\s*=\s*time\.(Minute)$/m],
  splitRaffleDefaultWinners: [draw, /\bMultiRaffleDefaultWinners:\s*(\d+),/],
  raffleBareMode: [raffleCommand, /^func \(rc raffleCommands\) bare\([^\n]*\{(?:(?!^\})[\s\S])*?default:\s*return rc\.(single|multi)\(/m],
  raffleMidCountEnabled: [draw, /\bMidCountDefault:\s*(true|false),/]
};

const pins = [
  { name: 'FreezeMaximum', web: FREEZE_MAX_MINUTES, source: loyalty, go: /^\s*FreezeMaximum\s*=\s*(\d+)\s*\*\s*time\.Minute\s*$/m },
  ...Object.entries(GIVEAWAY_LIMITS).map(([key, web]) => {
    const name = key[0].toUpperCase() + key.slice(1);
    const [source, go] = rafflePins[key as keyof typeof GIVEAWAY_LIMITS] ?? [
      key === 'maxWinners' ? drawRpc : giveawayPolicy,
      new RegExp('\\b' + name + '\\s*[:=]\\s*(true|false|[\\d_]+|time\\.Second)(?:\\s*\\*\\s*time\\.(\\w+))?(?=\\s*(?:,|$))', 'm')
    ];
    return { name, web, source, go };
  })
];

function goNumber(raw: string, unit: string | undefined): number {
  return (units[raw] ?? Number(raw.replaceAll('_', ''))) * (unit ? units[unit] : 1);
}

function goValue(match: RegExpExecArray | null): number | boolean | string | undefined {
  if (!match) return undefined;
  const [, raw, unit] = match;
  return goLiterals.get(raw) ?? goNumber(raw, unit);
}

test.each(pins)('$name matches the Go policy', ({ web, source, go }) => {
  expect(goValue(go.exec(source))).toBe(web);
});
