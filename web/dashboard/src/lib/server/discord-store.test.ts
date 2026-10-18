// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

let reply: Record<string, unknown> = {};
const calls: { subject: string; payload: unknown }[] = [];

const realNats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...realNats,
  rpcReply: async (subject: string, payload: unknown) => {
    calls.push({ subject, payload });
    return reply;
  }
}));

const { DISCORD_CODES, SETUP_FIELDS, listGuildModules, setGuildModule } = await import('./discord-store');

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

describe('discord module rpc', () => {
  const target = { userId: '42', guildId: '123456789012345678' };

  test('listGuildModules keys rows by name and keeps only module fields', async () => {
    reply = {
      found: true,
      modules: [
        { name: 'clips', enabled: true, settings: { clipsChannelId: '1', liveChannelId: '2' }, version: 3 },
        { name: 'levels', enabled: false, version: 1 },
        { name: '', enabled: true }
      ]
    };
    const out = await listGuildModules(target);
    expect(calls.at(-1)).toEqual({
      subject: 'bagel.rpc.dingress.discord.module.list',
      payload: { user_id: '42', guild_id: target.guildId }
    });
    expect(out).toEqual({
      found: true,
      modules: {
        clips: { enabled: true, settings: { clipsChannelId: '1' }, version: 3 },
        levels: { enabled: false, settings: {}, version: 1 }
      }
    });
  });

  test('listGuildModules throws a coded refusal on error', async () => {
    reply = { error: 'not your guild', code: 'forbidden' };
    await expect(listGuildModules(target)).rejects.toMatchObject({ name: 'DiscordRefusal', code: 'forbidden' });
  });

  test('setGuildModule sends the row and returns the new version', async () => {
    reply = { version: 4 };
    const out = await setGuildModule({
      ...target,
      name: 'clips',
      enabled: true,
      settings: { clipsChannelId: '1' },
      expectedVersion: 3
    });
    expect(calls.at(-1)).toEqual({
      subject: 'bagel.rpc.dingress.discord.module.set',
      payload: {
        user_id: '42',
        guild_id: target.guildId,
        name: 'clips',
        enabled: true,
        settings: { clipsChannelId: '1' },
        expected_version: 3
      }
    });
    expect(out).toEqual({ version: 4, code: '', error: '', fields: [] });
  });

  test('setGuildModule maps conflict and invalid with field names', async () => {
    const save = { ...target, name: 'clips', enabled: true, settings: {}, expectedVersion: 0 };
    reply = { error: 'stale', code: 'conflict' };
    expect((await setGuildModule(save)).code).toBe('conflict');
    reply = { error: 'bad', code: 'invalid', fields: ['clipsChannelId', 7] };
    expect(await setGuildModule(save)).toMatchObject({ code: 'invalid', fields: ['clipsChannelId'] });
  });
});
