// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const { DISCORD_CODES, SETUP_FIELDS, SETUP_REQUEST_KEYS, TEMPLATE_ITEM_KEYS } = await import('./discord-store');

const wire = JSON.parse(readFileSync(join(import.meta.dir,
  '../../../../../internal/domain/rpc/outgress/testdata/wire.json'), 'utf8'));

describe('discord setup wire contract', () => {
  test('SETUP_FIELDS reads every id the Go setup reply sends', () => {
    const goIds = Object.keys(wire.setup_reply).filter((key) => key.endsWith('_id')).sort();
    const tsIds: string[] = SETUP_FIELDS.map(([, key]) => key).sort();
    expect(tsIds).toEqual(goIds);
  });

  test('DISCORD_CODES lists every non-success Go code', () => {
    const goCodes = (wire.codes as string[]).filter((code) => code !== '').sort();
    const tsCodes: string[] = [...DISCORD_CODES].sort();
    expect(tsCodes).toEqual(goCodes);
  });

  test('TEMPLATE_ITEM_KEYS reads every key of a Go template item', () => {
    expect([...TEMPLATE_ITEM_KEYS].map(String).sort()).toEqual(Object.keys(wire.template_item).sort());
  });

  test('setup sends only keys the Go setup request knows', () => {
    const goKeys = Object.keys(wire.setup_request);
    for (const key of SETUP_REQUEST_KEYS) expect(goKeys).toContain(key);
  });
});
