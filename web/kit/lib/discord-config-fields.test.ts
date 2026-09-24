// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
