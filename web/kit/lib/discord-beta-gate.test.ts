// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { DISCORD_MODULE } from './catalog/discord';

const GO_BETA = join(import.meta.dir, '../../../internal/domain/discord/beta.go');
const GUILD_ROUTE = join(import.meta.dir, '../../dashboard/src/routes/(app)/discord/[guildId]');

function goBetaPremiumOnly(): boolean {
  const src = readFileSync(GO_BETA, 'utf8');
  const m = src.match(/^const BetaPremiumOnly = (true|false)$/m);
  if (!m) throw new Error('BetaPremiumOnly not found in beta.go (declaration shape changed?)');
  return m[1] === 'true';
}

describe('discord beta gate', () => {
  test('the catalog flag and the Go constant agree', () => {
    expect(DISCORD_MODULE.beta === true).toBe(goBetaPremiumOnly());
  });

  test('every guild page with actions builds them from the gated tables', () => {
    const files = readdirSync(GUILD_ROUTE, { recursive: true, encoding: 'utf8' })
      .filter((f) => f.endsWith('+page.server.ts'))
      .map((f) => join(GUILD_ROUTE, f))
      .filter((f) => /export const actions\b/.test(readFileSync(f, 'utf8')));

    expect(files.length).toBeGreaterThanOrEqual(3);

    for (const file of files) {
      const src = readFileSync(file, 'utf8');
      expect(src).toMatch(/import \{[^}]*\b(guildActions|moduleActions)\b[^}]*\} from '#lib\/server\/discord-guild'/);
      expect(src).toMatch(/export const actions(: Actions)? = (guildActions|moduleActions|\{)/);
    }
  });
});
