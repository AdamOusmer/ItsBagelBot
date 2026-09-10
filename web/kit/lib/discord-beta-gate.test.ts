// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The Discord beta gate is two flags in two languages, and they enforce
// different halves of the same rule. The catalog's `beta: true` shows the Beta
// chip, locks the tile, and closes /discord and its form actions through
// betaRouteDef. Go's BetaPremiumOnly stops the ENGINE serving a guild at all.
//
// Neither implies the other, and the drift that matters is silent in exactly
// one direction: with the catalog flag set and the Go constant false, the
// dashboard tells a free channel Discord is premium-only while the bot keeps
// running their server. Nothing fails, nobody reports it, and the beta is
// effectively open.
//
// This reads the Go constant out of the source for the same reason
// go-registry.test.ts does: the contract runs across languages, and a
// generated artifact is one more thing that can go stale between them.

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
    // betaRouteDef matches on href, so this is what makes the route guard
    // cover /discord and every form action under it.
    expect(DISCORD_MODULE.href).toBe('/discord');
  });

  // The guild section is seven routes, and each one exposes the SAME action
  // table by re-exporting guildActions. A child that declares its own actions
  // instead would get its own gate story: `save` there would skip the
  // assertModuleUnlocked check every guildActions entry runs, and a downgraded
  // board's stale form would write. This asserts the shape rather than the
  // behaviour because the behaviour lives behind a SvelteKit request event.
  test('every guild sub-page re-exports the one gated action table', () => {
    const files = readdirSync(GUILD_ROUTE, { withFileTypes: true })
      .filter((e) => e.isDirectory())
      .map((e) => join(GUILD_ROUTE, e.name, '+page.server.ts'))
      .filter((f) => existsSync(f));

    // The overview's own +page.server.ts, plus one per sub-page.
    files.push(join(GUILD_ROUTE, '+page.server.ts'));
    expect(files.length).toBeGreaterThanOrEqual(7);

    for (const file of files) {
      const src = readFileSync(file, 'utf8');
      expect(src).toContain("import { guildActions } from '$lib/server/discord-guild'");
      expect(src).toMatch(/export const actions(: Actions)? = guildActions;/);
    }
  });
});
