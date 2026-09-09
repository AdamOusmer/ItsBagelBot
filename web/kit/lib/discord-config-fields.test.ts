// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The console half of the Discord config field pin. Go's half is
// internal/domain/discord/configfields_test.go, and both read the SAME file:
// internal/domain/discord/testdata/config_fields.json.
//
// One fixture rather than two lists that "should" match, because two lists is
// exactly the arrangement that produced the drift this guards against. Adding
// a setting is now three edits — the Go struct, DiscordConfig, and the fixture
// — and skipping any one of them fails a test on the side that was skipped.

import { describe, expect, it } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { DISCORD_CONFIG_KEYS } from './discord-config';

const FIXTURE = join(import.meta.dir, '../../../internal/domain/discord/testdata/config_fields.json');

describe('discord config field pin', () => {
  it('DISCORD_CONFIG_KEYS matches the shared fixture Go reads', () => {
    const fixture: string[] = JSON.parse(readFileSync(FIXTURE, 'utf8')).fields;
    expect([...DISCORD_CONFIG_KEYS].sort()).toEqual([...fixture].sort());
  });

  it('the fixture is sorted, so a diff on it reads as an added or removed field', () => {
    const fixture: string[] = JSON.parse(readFileSync(FIXTURE, 'utf8')).fields;
    expect(fixture).toEqual([...fixture].sort());
  });
});
