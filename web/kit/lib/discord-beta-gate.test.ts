// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
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

  test('the gate closes the whole section, not just a tile', () => {
    expect(DISCORD_MODULE.href).toBe('/discord');
  });

  test('every guild sub-page re-exports the one gated action table', () => {
    const files = readdirSync(GUILD_ROUTE, { withFileTypes: true })
      .filter((e) => e.isDirectory())
      .map((e) => join(GUILD_ROUTE, e.name, '+page.server.ts'))
      .filter((f) => existsSync(f));

    files.push(join(GUILD_ROUTE, '+page.server.ts'));
    expect(files.length).toBeGreaterThanOrEqual(7);

    for (const file of files) {
      const src = readFileSync(file, 'utf8');
      expect(src).toContain("import { guildActions } from '$lib/server/discord-guild'");
      expect(src).toMatch(/export const actions(: Actions)? = guildActions;/);
    }
  });
});
