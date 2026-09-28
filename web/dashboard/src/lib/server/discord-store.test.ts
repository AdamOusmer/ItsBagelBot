// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

mock.module('newrelic', () => ({ default: { noticeError: () => {} } }));
const { MOD } = await import('@bagel/kit/catalog');
const discordConfig = await import('../../../../kit/lib/discord-config');
mock.module('@bagel/kit', () => ({ MOD, ...discordConfig }));
mock.module('./services', () => ({ SUB: {} }));
mock.module('./commands-store', () => ({ upsertModule: async () => {} }));
mock.module('./module-blob', () => ({ readModuleBlob: async () => ({ enabled: false, configs: {} }) }));

const { DISCORD_CODES, SETUP_FIELDS } = await import('./discord-store');

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
});
