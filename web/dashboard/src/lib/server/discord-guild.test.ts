// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const realStore = await import('./discord-store');
const kit = await import('@bagel/kit');
const GUILD = '123456789012345678';
const legacyConfig = { ...kit.blankDiscordConfig(), twitchLogin: 'old' };

type ModuleList = { found: boolean; modules: Record<string, unknown> };

let lists: (ModuleList | Error)[] = [];
const saves: unknown[] = [];
const moduleSaves: unknown[] = [];
let saveReply = { version: 1, code: '', error: '' };

mock.module('$lib/server/discord-store', () => ({
  ...realStore,
  readDiscord: async () => ({
    enabled: true,
    twitchLogin: 'bagel',
    guilds: [{ guildId: GUILD, name: 'g' }],
    truncated: false
  }),
  readLegacyBlob: async () => ({}),
  listGuildModules: async () => {
    const next = lists.length > 1 ? lists.shift()! : lists[0]!;
    if (next instanceof Error) throw next;
    return next;
  },
  guildLayout: async () => realStore.blankLayout(),
  botStatus: async () => realStore.blankStatus(),
  saveGuildConfig: async (save: unknown) => {
    saves.push(save);
    return saveReply;
  },
  saveDiscordModule: async (save: unknown) => {
    moduleSaves.push(save);
  }
}));
mock.module('$lib/server/module-gate', () => ({
  gateModulePage: () => {},
  moduleLocked: async () => false,
  assertModuleUnlocked: async () => true
}));
mock.module('$lib/server/discord-oauth', () => ({ discordConfigured: () => true, requireDiscordActor: () => {} }));
mock.module('$lib/server/services', () => ({ auditDashboardImpersonation: () => {} }));
mock.module('$lib/server/board', () => ({ effectiveId: () => '42' }));
mock.module('@bagel/kit', () => ({ ...kit, legacyConfigFor: () => legacyConfig }));

const { loadGuildShell, loadGuildModule } = await import('./discord-guild');

const event = {
  cookies: { get: () => undefined, set: () => {}, delete: () => {} },
  locals: { session: { user_id: '42' } },
  params: { guildId: GUILD },
  url: new URL('http://localhost/discord')
} as never;

const unmigrated: ModuleList = { found: false, modules: {} };
const migrated: ModuleList = { found: true, modules: { clips: { enabled: true, settings: {}, version: 5 } } };

beforeEach(() => {
  saves.length = 0;
  moduleSaves.length = 0;
  saveReply = { version: 1, code: '', error: '' };
});

describe('legacy discord migration on load', () => {
  test('shell saves the legacy config once and re-reads the module rows', async () => {
    lists = [unmigrated, migrated];
    const page = await loadGuildShell(event);
    expect(saves).toHaveLength(1);
    expect(saves[0]).toMatchObject({ userId: '42', guildId: GUILD, expectedVersion: kit.DISCORD_CONFIG_VERSION_NEW });
    expect(moduleSaves).toHaveLength(1);
    expect(page.found).toBe(true);
  });

  test('module page shows the migrated row version', async () => {
    lists = [unmigrated, migrated];
    const page = await loadGuildModule(event, 'clips');
    expect(saves).toHaveLength(1);
    expect(page.found).toBe(true);
    expect(page.version).toBe(5);
  });

  test('a degraded list read does not migrate', async () => {
    lists = [new Error('down')];
    const page = await loadGuildShell(event);
    expect(saves).toHaveLength(0);
    expect(page.degraded).toBe(true);
  });

  test('a refused save leaves the page unmigrated', async () => {
    lists = [unmigrated];
    saveReply = { version: 0, code: 'conflict', error: 'stale' };
    const page = await loadGuildShell(event);
    expect(moduleSaves).toHaveLength(0);
    expect(page.found).toBe(false);
  });

  test('an already-migrated guild is left alone', async () => {
    lists = [migrated];
    await loadGuildShell(event);
    expect(saves).toHaveLength(0);
  });
});
