// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { DISCORD_CONFIG_KEYS } from './discord-config';
import {
  DISCORD_MODULES,
  discordModuleById,
  parseDiscordModuleSettings,
  tDiscordModuleField,
  tDiscordModuleLabel,
  validateDiscordModule
} from './discord-modules';

// Mirrors internal/domain/discord Modules (modules.go + settings.go json tags), identity and voicestate excluded.
const GO_MODULES: [string, string, string[]][] = [
  ['roles', 'core', ['ownerRoleId', 'leadModRoleId', 'modsRoleId', 'vipRoleId', 'subscriberRoleId', 'regularsRoleId', 'pinnedRoles']],
  ['live', 'default', ['liveChannelId', 'categoryAllow', 'categoryDeny', 'twitchLogin']],
  ['clips', 'default', ['clipsChannelId']],
  ['welcome', 'default', ['welcomeChannelId']],
  ['goodbye', 'optin', []],
  ['autorole', 'default', ['memberRoleId']],
  ['subscribers', 'optin', ['subsChannelId', 'subsCategoryId', 'vipChannelId', 'vipCategoryId']],
  ['voice', 'default', ['voiceHubId', 'voiceCategoryId', 'voiceNameTemplate', 'voiceUserLimit', 'voicePrivacy']],
  [
    'tickets',
    'default',
    [
      'ticketChannelId',
      'ticketCategoryId',
      'ticketArchiveCategoryId',
      'ticketStaffRoleIds',
      'ticketOpenLimit',
      'ticketTranscriptEnabled',
      'ticketLogChannelId',
      'ticketPanelTitle',
      'ticketPanelBody',
      'ticketPanelColor',
      'ticketPanelButton'
    ]
  ],
  [
    'logs',
    'default',
    [
      'logChannelId',
      'logMessagesEnabled',
      'logMembersEnabled',
      'logVoiceEnabled',
      'logModerationEnabled',
      'logChannelsEnabled',
      'logRolesEnabled',
      'logServerEnabled',
      'logIgnoreBots',
      'logMessagesChannelId',
      'logMembersChannelId',
      'logVoiceChannelId',
      'logModerationChannelId',
      'logIgnoredChannelIds'
    ]
  ],
  ['levels', 'default', []],
  ['linkguard', 'optin', ['linkAllowList']],
  ['moderation', 'default', []]
];

const ENABLE_KEYS = [
  'liveEnabled',
  'clipsEnabled',
  'welcomeEnabled',
  'goodbyeEnabled',
  'autoRoleEnabled',
  'subscribersEnabled',
  'voiceEnabled',
  'ticketsEnabled',
  'logsEnabled',
  'levelsEnabled',
  'linkGuardEnabled'
];

describe('discord module catalog', () => {
  test('ids, kinds and field keys mirror the Go module table', () => {
    const got = DISCORD_MODULES.map((def) => [def.id, def.kind, def.fields.map((f) => f.key)]);
    expect(got).toEqual(GO_MODULES);
  });

  test('every config key but guildId and the enable keys belongs to exactly one module', () => {
    const owned = DISCORD_MODULES.flatMap((def) => def.fields.map((f) => f.key as string));
    expect(new Set(owned).size).toBe(owned.length);
    const expected = DISCORD_CONFIG_KEYS.filter((key) => key !== 'guildId' && !ENABLE_KEYS.includes(key));
    expect([...owned].sort()).toEqual([...expected].sort());
  });

  test('only tickets links to a dedicated page', () => {
    expect(DISCORD_MODULES.filter((def) => def.href).map((def) => [def.id, def.href])).toEqual([['tickets', 'tickets']]);
  });

  test('copy falls back to English and prefers the catalog', () => {
    const def = discordModuleById('voice')!;
    const missing = (key: string) => key;
    expect(tDiscordModuleLabel(missing as never, def)).toBe('Voice rooms');
    const hub = def.fields[0];
    const t = (key: string) => (key === 'discord.modules.voice.fields.voiceHubId.label' ? 'Hub' : key);
    expect(tDiscordModuleField(t as never, 'voice', hub, 'label')).toBe('Hub');
    expect(tDiscordModuleField(missing as never, 'voice', hub, 'help')).toBe('');
  });
});

describe('validateDiscordModule', () => {
  test('rejects a bad snowflake and keeps the previous value', () => {
    const r = validateDiscordModule('clips', { clipsChannelId: 'not-an-id' }, { clipsChannelId: '123456789012345678' });
    expect(r?.errors).toEqual([{ field: 'clipsChannelId', code: 'snowflake' }]);
    expect(r?.settings).toEqual({ clipsChannelId: '123456789012345678' });
  });

  test('accepts and normalizes valid values', () => {
    const r = validateDiscordModule('tickets', {
      ticketStaffRoleIds: '123456789012345678, 123456789012345678,223456789012345678',
      ticketPanelColor: '#ABC',
      ticketOpenLimit: '3'
    });
    expect(r?.errors).toEqual([]);
    expect(r?.settings.ticketStaffRoleIds).toBe('123456789012345678,223456789012345678');
    expect(r?.settings.ticketPanelColor).toBe('#aabbcc');
    expect(r?.settings.ticketOpenLimit).toBe('3');
  });

  test('ignores keys owned by another module', () => {
    const r = validateDiscordModule('clips', { clipsChannelId: '', liveChannelId: 'bad' });
    expect(r?.errors).toEqual([]);
    expect(Object.keys(r!.settings)).toEqual(['clipsChannelId']);
  });

  test('rejects a bad choice and flag', () => {
    expect(validateDiscordModule('voice', { voicePrivacy: 'secret' })?.errors).toEqual([
      { field: 'voicePrivacy', code: 'choice' }
    ]);
    expect(validateDiscordModule('tickets', { ticketTranscriptEnabled: 'maybe' })?.errors).toEqual([
      { field: 'ticketTranscriptEnabled', code: 'flag' }
    ]);
  });

  test('unknown module is null; identity has no page', () => {
    expect(validateDiscordModule('nope', {})).toBeNull();
    expect(validateDiscordModule('identity', {})).toBeNull();
  });

  test('parseDiscordModuleSettings keeps only string module fields', () => {
    expect(parseDiscordModuleSettings('clips', { clipsChannelId: '1', liveChannelId: '2', x: 3 })).toEqual({
      clipsChannelId: '1'
    });
    expect(parseDiscordModuleSettings('clips', null)).toEqual({ clipsChannelId: '' });
  });
});
