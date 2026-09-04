// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The Discord bot invite permission integer exists TWICE, in two languages:
// internal/domain/discord.BotPermissions builds it as a bit expression for the
// Go services, and console/dashboard/.../discord-oauth.ts carries the same
// number as a decimal literal because the dashboard is what actually builds
// the invite URL a streamer clicks.
//
// Nothing but this test ties them together, and a drift is silent in the worst
// possible way: the dashboard keeps minting invite URLs with the OLD integer,
// so every guild that installs after the Go-side change is missing the
// permission the Go code now assumes it has. The failure appears much later as
// a 403 on one endpoint in some guilds and not others, with nothing pointing
// back at the edit that caused it. Discord also freezes permissions into the
// bot's role at install time, so those guilds do not self-heal: they each need
// a re-authorization.
//
// The test reads the Go source rather than a generated manifest, matching
// catalog/go-registry.test.ts and rejecting the same alternatives for the same
// reasons: a Go test reading .ts inverts the dependency, and a checked-in JSON
// artifact is one more thing that can go stale between the two.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const GO_TEMPLATE = join(import.meta.dir, '../../../internal/domain/discord/template.go');
const TS_OAUTH = join(import.meta.dir, '../../dashboard/src/lib/server/discord-oauth.ts');

// evalBitExpr evaluates the subset of Go the constant is written in: decimal
// terms and `1<<N` shifts, OR-ed together. BigInt because bit 40 overflows a
// 32-bit intermediate, and Number's bitwise operators truncate to 32 bits --
// evaluating this with `|` would silently produce the wrong answer for exactly
// the high bit the expression cares about (MODERATE_MEMBERS).
function evalBitExpr(expr: string): bigint {
  return expr
    .split('|')
    .map((term) => term.trim())
    .reduce((acc, term) => {
      const shift = term.match(/^1\s*<<\s*(\d+)$/);
      if (shift) return acc | (1n << BigInt(shift[1]));
      if (!/^\d+$/.test(term)) throw new Error(`unparsable term in BotPermissions: ${term}`);
      return acc | BigInt(term);
    }, 0n);
}

function goPermissions(): bigint {
  const src = readFileSync(GO_TEMPLATE, 'utf8');
  const m = src.match(/^const BotPermissions = (.+)$/m);
  if (!m) throw new Error('BotPermissions not found in template.go (declaration shape changed?)');
  return evalBitExpr(m[1]);
}

function tsPermissions(): bigint {
  const src = readFileSync(TS_OAUTH, 'utf8');
  const m = src.match(/^export const DISCORD_BOT_PERMISSIONS = (\d+);$/m);
  if (!m) throw new Error('DISCORD_BOT_PERMISSIONS not found in discord-oauth.ts');
  return BigInt(m[1]);
}

describe('discord bot permissions', () => {
  test('the Go expression and the dashboard literal are the same number', () => {
    expect(tsPermissions()).toBe(goPermissions());
  });

  test('the invite never requests Administrator', () => {
    // Administrator (1<<3) would make every other bit in this integer
    // meaningless and hands a compromised bot token the whole guild. The
    // channel/role template is built from specific grants precisely so this
    // stays off.
    expect(goPermissions() & 8n).toBe(0n);
  });

  test('the invite requests CHANGE_NICKNAME', () => {
    // Without it the per-guild rename to "ItsBagelBot - Premium" 403s while
    // the avatar half of the same call still succeeds, which looks like a bug
    // in the feature rather than a missing grant.
    expect(goPermissions() & (1n << 26n)).toBe(1n << 26n);
  });

  test('the number stays inside a safe JS integer', () => {
    // The dashboard hands this to URLSearchParams as a Number. Past
    // Number.MAX_SAFE_INTEGER it would round, and the minted invite would ask
    // for a different permission set than the one written here.
    expect(goPermissions() <= BigInt(Number.MAX_SAFE_INTEGER)).toBe(true);
  });
});
