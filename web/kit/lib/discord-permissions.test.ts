// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const GO_TEMPLATE = join(import.meta.dir, '../../../internal/domain/discord/template.go');
const TS_OAUTH = join(import.meta.dir, '../../dashboard/src/lib/server/discord-oauth.ts');

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
    expect(goPermissions() & 8n).toBe(0n);
  });

  test('the invite requests CHANGE_NICKNAME', () => {
    expect(goPermissions() & (1n << 26n)).toBe(1n << 26n);
  });

  test('the number stays inside a safe JS integer', () => {
    expect(goPermissions() <= BigInt(Number.MAX_SAFE_INTEGER)).toBe(true);
  });
});
