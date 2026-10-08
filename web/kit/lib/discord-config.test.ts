// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  CATEGORY_NAME_MAX,
  DISCORD_CONFIG_KEYS,
  DISCORD_CONFIG_VERSION_NEW,
  DISCORD_MANAGE_GUILD,
  LIVE_COLOR_HEX,
  PINNED_SLOTS,
  TICKET_OPEN_LIMIT_DEFAULT,
  TICKET_PANEL_BODY_MAX,
  TICKET_PANEL_DEFAULTS,
  blankDiscordConfig,
  type DiscordConfig,
  canManageGuild,
  clearPinnedSlots,
  droppedPinNotice,
  fieldErrorsByField,
  encodeIdList,
  encodeNameList,
  encodePinnedRoles,
  guildBotState,
  guildIconSrc,
  guildIconURL,
  guildMonogram,
  guildPickerBadge,
  hexToDiscordColor,
  legacyConfigFor,
  mergeDiscordConfig,
  normalizeHex,
  parseConfigVersion,
  parseDiscordConfig,
  parseIdList,
  parseUserGuild,
  parseUserGuilds,
  parseNameList,
  parsePinnedRoles,
  logCategoryOn,
  logChannelFor,
  logIgnoreBotsOn,
  logIgnores,
  ticketOpenLimitN,
  ticketPanelPayload,
  ticketPanelSpec,
  ticketStaffRoleIds,
  voiceLimit,
  voiceName,
  voicePrivacy
} from './discord-config';

const ID_A = '123456789012345678';
const ID_B = '234567890123456789';
const ID_C = '345678901234567890';
const ids = (n: number) => Array.from({ length: n }, (_, i) => `10000000000000${String(i).padStart(4, '0')}`).join(',');

describe('parse / round trip', () => {
  test('a blank config has every declared key, all empty', () => {
    const blank = blankDiscordConfig();
    expect(Object.keys(blank).sort()).toEqual([...DISCORD_CONFIG_KEYS].sort());
    expect(Object.values(blank).every((v) => v === '')).toBe(true);
  });

  test('parse keeps string fields and drops everything else', () => {
    const parsed = parseDiscordConfig({
      guildId: ID_A,
      ticketOpenLimit: '3',
      levelsEnabled: 42,
      unknownKey: 'nope'
    });
    expect(parsed.guildId).toBe(ID_A);
    expect(parsed.ticketOpenLimit).toBe('3');
    expect(parsed.levelsEnabled).toBe('');
    expect(Object.keys(parsed)).not.toContain('unknownKey');
  });

  test('parse survives a null, an array and a scalar', () => {
    expect(parseDiscordConfig(null)).toEqual(blankDiscordConfig());
    expect(parseDiscordConfig([1, 2])).toEqual(blankDiscordConfig());
    expect(parseDiscordConfig('nope')).toEqual(blankDiscordConfig());
  });

  test('parse(merge(x)) round-trips a full config unchanged', () => {
    const seed = {
      ...blankDiscordConfig(),
      guildId: ID_A,
      liveChannelId: ID_B,
      ticketStaffRoleIds: `${ID_A},${ID_B}`,
      pinnedRoles: `owner=${ID_A},vip=${ID_B}`,
      ticketPanelColor: '#1a2b3c',
      ticketOpenLimit: '4',
      levelsEnabled: 'off'
    };
    const merged = mergeDiscordConfig(blankDiscordConfig(), seed);
    expect(merged.errors).toEqual([]);
    expect(merged.config).toEqual(seed);
    expect(parseDiscordConfig(JSON.parse(JSON.stringify(merged.config)))).toEqual(seed);
  });
});

describe('merge', () => {
  interface Row {
    name: string;
    current: Partial<DiscordConfig>;
    draft: Record<string, unknown>;
    want: Partial<DiscordConfig>;
    errors: ReturnType<typeof mergeDiscordConfig>['errors'];
  }

  const row = (name: string, current: Row['current'], draft: Row['draft'], want: Row['want'], errors: Row['errors']): Row => ({ name, current, draft, want, errors });

  const ROWS: Row[] = [
    row('a field the draft omits keeps its stored value', { liveChannelId: ID_A }, { clipsChannelId: ID_B }, { liveChannelId: ID_A, clipsChannelId: ID_B }, []),
    row('a bad snowflake keeps the stored value and reports the field', { liveChannelId: ID_A }, { liveChannelId: '12' }, { liveChannelId: ID_A }, [{ field: 'liveChannelId', code: 'snowflake' }]),
    row('an empty string clears a field', { liveChannelId: ID_A }, { liveChannelId: '' }, { liveChannelId: '' }, []),
    row('non-string draft values are ignored, not coerced', { ticketOpenLimit: '3' }, { ticketOpenLimit: 5 }, { ticketOpenLimit: '3' }, []),
    row('lists and colours are canonicalised on the way in', {}, { ticketStaffRoleIds: ` ${ID_A} , ${ID_B} , ${ID_A} `, ticketPanelColor: 'C47A3A', pinnedRoles: ` vip = ${ID_B} , owner = ${ID_A} ` }, { ticketStaffRoleIds: `${ID_A},${ID_B}`, ticketPanelColor: LIVE_COLOR_HEX, pinnedRoles: `owner=${ID_A},vip=${ID_B}` }, []),
    row('a malformed role id in a list is refused, not silently dropped', { ticketStaffRoleIds: ID_A }, { ticketStaffRoleIds: `${ID_B},notasnowflake` }, { ticketStaffRoleIds: ID_A }, [{ field: 'ticketStaffRoleIds', code: 'list' }]),
    row('an unknown pin slot is refused, not silently dropped', {}, { pinnedRoles: `nosuchslot=${ID_A}` }, { pinnedRoles: '' }, [{ field: 'pinnedRoles', code: 'pinned' }]),
    row('a malformed role id in a pin pair is refused', {}, { pinnedRoles: 'mods=notasnowflake' }, { pinnedRoles: '' }, [{ field: 'pinnedRoles', code: 'pinned' }]),
    row('a colour that is not a colour is refused rather than swapped', { ticketPanelColor: '#52b788' }, { ticketPanelColor: 'rebeccapurple' }, { ticketPanelColor: '#52b788' }, [{ field: 'ticketPanelColor', code: 'color' }]),
    row('the colour shorthand still normalises', {}, { ticketPanelColor: 'ABC' }, { ticketPanelColor: '#aabbcc' }, []),
    row('a flag only accepts on/off', {}, { levelsEnabled: 'yes' }, { levelsEnabled: '' }, [{ field: 'levelsEnabled', code: 'flag' }]),
    row('an over-long panel body is refused, not truncated', {}, { ticketPanelBody: 'x'.repeat(TICKET_PANEL_BODY_MAX + 1) }, { ticketPanelBody: '' }, [{ field: 'ticketPanelBody', code: 'length' }]),
    ...['0', '6', '-1', 'two', '3.5'].map((bad) => row(`the open limit ${JSON.stringify(bad)} is refused outside 1..5`, {}, { ticketOpenLimit: bad }, { ticketOpenLimit: '' }, [{ field: 'ticketOpenLimit', code: 'range' }])),
    row('the open limit 5 is accepted', {}, { ticketOpenLimit: '5' }, { ticketOpenLimit: '5' }, []),
    row('a log category flag only accepts on/off', {}, { logVoiceEnabled: 'maybe' }, { logVoiceEnabled: '' }, [{ field: 'logVoiceEnabled', code: 'flag' }]),
    row('a per-category log channel must be a snowflake', {}, { logMembersChannelId: 'logs' }, { logMembersChannelId: '' }, [{ field: 'logMembersChannelId', code: 'snowflake' }]),
    row('the ignored channel list is canonicalised', {}, { logIgnoredChannelIds: ` ${ID_A} , ${ID_B} , ${ID_A} ` }, { logIgnoredChannelIds: `${ID_A},${ID_B}` }, []),
    row('a bad ignored channel is refused', {}, { logIgnoredChannelIds: `${ID_A},x` }, { logIgnoredChannelIds: '' }, [{ field: 'logIgnoredChannelIds', code: 'list' }]),
    row('25 ignored channels are accepted', {}, { logIgnoredChannelIds: ids(25) }, { logIgnoredChannelIds: ids(25) }, []),
    row('26 ignored channels are refused', {}, { logIgnoredChannelIds: ids(26) }, { logIgnoredChannelIds: '' }, [{ field: 'logIgnoredChannelIds', code: 'list' }]),
    row('a voice name over the max is refused', {}, { voiceNameTemplate: 'x'.repeat(101) }, { voiceNameTemplate: '' }, [{ field: 'voiceNameTemplate', code: 'length' }]),
    ...['100', '-1', '1.5', 'x', '007'].map((bad) => row(`the voice limit ${JSON.stringify(bad)} is refused outside 0..99`, {}, { voiceUserLimit: bad }, { voiceUserLimit: '' }, [{ field: 'voiceUserLimit', code: 'range' }])),
    row('the voice limit 0 and 99 are accepted', {}, { voiceUserLimit: '0' }, { voiceUserLimit: '0' }, []),
    row('a padded voice limit is accepted', {}, { voiceUserLimit: ' 5' }, { voiceUserLimit: '5' }, []),
    row('a trailing comma in the ignored channel list is accepted', {}, { logIgnoredChannelIds: `${ID_A},${ID_B},` }, { logIgnoredChannelIds: `${ID_A},${ID_B}` }, []),
    row('an unknown voice privacy is refused', {}, { voicePrivacy: 'secret' }, { voicePrivacy: '' }, [{ field: 'voicePrivacy', code: 'choice' }]),
    row('a known voice privacy is accepted', {}, { voicePrivacy: 'hidden' }, { voicePrivacy: 'hidden' }, [])
  ];

  test.each(ROWS)('$name', ({ current, draft, want, errors }) => {
    const merged = mergeDiscordConfig({ ...blankDiscordConfig(), ...current }, draft);
    expect(merged.config).toMatchObject(want);
    expect(merged.errors).toEqual(errors);
  });
});

describe('pinned roles', () => {
  test('encode then parse is the identity for every slot', () => {
    const pins = Object.fromEntries(PINNED_SLOTS.map((s, i) => [s, `1234567890123456${10 + i}`]));
    const encoded = encodePinnedRoles(pins);
    expect(parsePinnedRoles(encoded)).toEqual(pins);
  });

  test('encode is stable in slot order regardless of insertion order', () => {
    expect(encodePinnedRoles({ vip: ID_B, owner: ID_A })).toBe(`owner=${ID_A},vip=${ID_B}`);
    expect(encodePinnedRoles({ owner: ID_A, vip: ID_B })).toBe(`owner=${ID_A},vip=${ID_B}`);
  });

  test('parse drops unknown slots and malformed ids', () => {
    expect(parsePinnedRoles(`owner=${ID_A},founder=${ID_B},vip=nope,=,mods=`)).toEqual({ owner: ID_A });
  });
});

describe('id lists', () => {
  test('parse trims, drops junk and dedupes; encode round-trips', () => {
    expect(parseIdList(` ${ID_A} , nope , ${ID_B} , ${ID_A} `)).toEqual([ID_A, ID_B]);
    expect(encodeIdList([ID_A, '12', ID_B])).toBe(`${ID_A},${ID_B}`);
    expect(encodeIdList([])).toBe('');
  });

  test('an id is a 17 to 20 digit number', () => {
    const ids = ['1'.repeat(16), '1'.repeat(17), '1'.repeat(20), '1'.repeat(21), '1234567890123456a'];
    expect(parseIdList(ids.join(','))).toEqual(['1'.repeat(17), '1'.repeat(20)]);
  });
});

describe('name lists', () => {
  test('parse trims, dedupes case-insensitively and drops over-long names', () => {
    expect(parseNameList(' Valorant , minecraft ,VALORANT, ')).toEqual(['Valorant', 'minecraft']);
    expect(parseNameList('x'.repeat(CATEGORY_NAME_MAX + 1))).toEqual([]);
    expect(parseNameList('x'.repeat(CATEGORY_NAME_MAX))).toHaveLength(1);
  });

  test('encode round-trips through parse', () => {
    expect(encodeNameList(['Valorant', 'Just Chatting'])).toBe('Valorant, Just Chatting');
    expect(parseNameList(encodeNameList(['a', 'b']))).toEqual(['a', 'b']);
    expect(encodeNameList([])).toBe('');
  });
});

describe('colour', () => {
  test('good hex normalises to lowercase #rrggbb', () => {
    expect(normalizeHex('#C47A3A')).toBe('#c47a3a');
    expect(normalizeHex('c47a3a')).toBe('#c47a3a');
    expect(normalizeHex('#abc')).toBe('#aabbcc');
  });

  test('bad hex falls back rather than painting a wrong colour', () => {
    for (const bad of ['', 'red', '#12', '#1234567', 'rgb(1,2,3)', '#gggggg']) {
      expect(normalizeHex(bad)).toBe(LIVE_COLOR_HEX);
    }
    expect(normalizeHex('nope', '#000000')).toBe('#000000');
  });
});

describe('accessors', () => {
  test('the panel spec falls back to the Bagel defaults field by field', () => {
    const spec = ticketPanelSpec({ ...blankDiscordConfig(), ticketPanelTitle: 'Support' });
    expect(spec).toEqual({
      title: 'Support',
      body: TICKET_PANEL_DEFAULTS.body,
      button: TICKET_PANEL_DEFAULTS.button,
      color: LIVE_COLOR_HEX
    });
  });

  test('the open limit defaults when unset or out of range', () => {
    expect(ticketOpenLimitN(blankDiscordConfig())).toBe(TICKET_OPEN_LIMIT_DEFAULT);
    expect(ticketOpenLimitN({ ...blankDiscordConfig(), ticketOpenLimit: '9' })).toBe(TICKET_OPEN_LIMIT_DEFAULT);
    expect(ticketOpenLimitN({ ...blankDiscordConfig(), ticketOpenLimit: '4' })).toBe(4);
  });

  test('ticket staff falls back to the three staff role slots', () => {
    const base = { ...blankDiscordConfig(), ownerRoleId: ID_A, leadModRoleId: ID_B };
    expect(ticketStaffRoleIds(base)).toEqual([ID_A, ID_B]);
    expect(ticketStaffRoleIds({ ...base, ticketStaffRoleIds: ID_C })).toEqual([ID_C]);
  });
});

describe('log and voice readers', () => {
  const blank = blankDiscordConfig();

  test('a category is on by default and needs the master switch', () => {
    expect(logCategoryOn(blank, 'roles')).toBe(true);
    expect(logCategoryOn({ ...blank, logRolesEnabled: 'off' }, 'roles')).toBe(false);
    expect(logCategoryOn({ ...blank, logsEnabled: 'off' }, 'roles')).toBe(false);
  });

  test('the category channel falls back to the general log channel', () => {
    const cfg = { ...blank, logChannelId: ID_A, logVoiceChannelId: ID_B };
    expect(logChannelFor(cfg, 'voice')).toBe(ID_B);
    expect(logChannelFor(cfg, 'messages')).toBe(ID_A);
    expect(logChannelFor(cfg, 'roles')).toBe(ID_A);
    expect(logChannelFor(blank, 'voice')).toBe('');
  });

  test('ignored channels and bots', () => {
    const cfg = { ...blank, logIgnoredChannelIds: `${ID_A},${ID_B}` };
    expect(logIgnores(cfg, ID_B)).toBe(true);
    expect(logIgnores(cfg, ID_C)).toBe(false);
    expect(logIgnores(cfg, '')).toBe(false);
    expect(logIgnoreBotsOn(blank)).toBe(true);
    expect(logIgnoreBotsOn({ ...blank, logIgnoreBots: 'off' })).toBe(false);
  });

  test('voice name, limit and privacy defaults', () => {
    expect(voiceName(blank, 'Ada')).toBe('Ada');
    expect(voiceName({ ...blank, voiceNameTemplate: "{owner}'s room" }, 'Ada')).toBe("Ada's room");
    expect(voiceName({ ...blank, voiceNameTemplate: 'x'.repeat(150) }, 'Ada')).toHaveLength(100);
    expect(voiceLimit(blank)).toBe(0);
    expect(voiceLimit({ ...blank, voiceUserLimit: '12' })).toBe(12);
    expect(voiceLimit({ ...blank, voiceUserLimit: '100' })).toBe(0);
    expect(voicePrivacy(blank)).toBe('open');
    expect(voicePrivacy({ ...blank, voicePrivacy: 'locked' })).toBe('locked');
    expect(voicePrivacy({ ...blank, voicePrivacy: 'bogus' })).toBe('open');
  });
});

describe('config version', () => {
  test.each([
    ['a number decodes', 7, 7],
    ['a quoted number decodes', '7', 7],
    ['a padded quoted number decodes', ' 12 ', 12],
    ['nothing reads as never-saved', undefined, DISCORD_CONFIG_VERSION_NEW],
    ['a negative reads as never-saved', -1, DISCORD_CONFIG_VERSION_NEW],
    ['a fraction reads as never-saved', 1.5, DISCORD_CONFIG_VERSION_NEW],
    ['text reads as never-saved', 'abc', DISCORD_CONFIG_VERSION_NEW],
    ['null reads as never-saved', null, DISCORD_CONFIG_VERSION_NEW],
    ['an unsafe integer reads as never-saved', Number.MAX_SAFE_INTEGER + 2, DISCORD_CONFIG_VERSION_NEW]
  ])('%s', (_name, raw, want) => {
    expect(parseConfigVersion(raw)).toBe(want);
  });
});

describe('guild permissions', () => {
  const PAST_2_53 = (1n << 50n) | DISCORD_MANAGE_GUILD;

  test.each([
    ['ADMINISTRATOR qualifies on its own', { permissions: '8' }, true],
    ['MANAGE_GUILD qualifies on its own', { permissions: '32' }, true],
    ['both bits qualify', { permissions: '40' }, true],
    ['a guild with neither bit is filtered out', { permissions: '3136' }, false],
    ['a zero bitfield is filtered out', { permissions: '0' }, false],
    ['a missing bitfield is filtered out', {}, false],
    ['owner qualifies whatever the bitfield says', { owner: true, permissions: '0' }, true],
    ['a non-owner with no bits is filtered out', { owner: false, permissions: '0' }, false],
    ['a bitfield past 2^53 keeps every low bit', { permissions: PAST_2_53.toString() }, true],
    ['a high bit alone is not a permission', { permissions: (1n << 50n).toString() }, false],
    ['trailing junk is worth no permission', { permissions: '12x' }, false],
    ['a negative is worth no permission', { permissions: '-8' }, false],
    ['a number is read as its bits', { permissions: 8 }, true],
    ['a fractional number is worth no permission', { permissions: 1.5 }, false]
  ] as [string, { owner?: boolean; permissions?: string | number }, boolean][])('%s', (_name, guild, want) => {
    expect(canManageGuild(guild)).toBe(want);
  });
});

describe('guild presentation', () => {
  test.each([
    ['Demo Bakery', 'DB'],
    ['  the  bagel  house ', 'TB'],
    ['Bagels', 'BA'],
    ['x', 'X'],
    ['   ', '?']
  ])('the monogram of %p is %s', (name, want) => {
    expect(guildMonogram(name)).toBe(want);
  });

  test.each([
    ['online when the bot is present', { botPresent: true }, 'online'],
    ['offline when the bot is absent', { botPresent: false }, 'offline'],
    ['reauth outranks offline on the bot pill', { botPresent: false, needsReauth: true }, 'reauth'],
    ['reauth outranks online on the bot pill', { botPresent: true, needsReauth: true }, 'reauth'],
    ['no signals read as offline', {}, 'offline']
  ] as [string, Parameters<typeof guildBotState>[0], ReturnType<typeof guildBotState>][])('%s', (_name, signals, want) => {
    expect(guildBotState(signals)).toBe(want);
  });

  test('an unread reauth flag is neutral, never green and never red', () => {
    expect(guildBotState({ botPresent: true, reauthUnknown: true })).toBe('unknown');
    expect(guildBotState({ botPresent: false, reauthUnknown: true })).toBe('unknown');
    expect(guildBotState({ botPresent: true, needsReauth: true, reauthUnknown: true })).toBe('reauth');
    expect(guildBotState({ botPresent: true, reauthUnknown: false })).toBe('online');
  });

  test('the picker badge separates my servers from someone else\'s', () => {
    const bound = [ID_A, ID_B];
    expect(guildPickerBadge(ID_A, { bound })).toBe('mine');
    expect(guildPickerBadge(ID_C, { bound })).toBe('addable');
    expect(guildPickerBadge(ID_A, { bound: [] })).toBe('addable');
    expect(guildPickerBadge(ID_C, { bound, elsewhere: [ID_C] })).toBe('elsewhere');
    expect(guildPickerBadge(ID_A, { bound, elsewhere: [ID_A] })).toBe('mine');
  });
});

describe('parseUserGuilds', () => {
  test('keeps the permission bitfield as the string Discord sent', () => {
    const guild = { id: ID_A, name: 'Demo Bakery', icon: '', owner: true, permissions: '1125899906842623' };
    expect(parseUserGuilds([guild])).toEqual([guild]);
  });

  test.each([
    ['a 429 body is not an empty guild list', { message: 'You are being rate limited.', retry_after: 1.5 }],
    ['an HTML error page is not an empty guild list', '<!DOCTYPE html><html><body>502</body></html>']
  ])('%s', (_name, body) => {
    expect(parseUserGuilds(body)).toBeNull();
  });

  test('every non-array body is refused rather than emptied', () => {
    for (const body of [null, undefined, 0, '', {}, { guilds: [] }]) {
      expect(parseUserGuilds(body)).toBeNull();
    }
  });

  test('junk entries are dropped, good ones survive', () => {
    const guilds = parseUserGuilds([null, 'x', {}, { id: '' }, { id: ID_B }, [ID_C]]);
    expect(guilds).toEqual([{ id: ID_B, name: '', icon: '', owner: false, permissions: '' }]);
  });

  test('a numeric permissions field is not trusted as a string', () => {
    expect(parseUserGuild({ id: ID_A, permissions: 8 })?.permissions).toBe('');
  });
});

describe('legacyConfigFor', () => {
  const legacy = {
    guildId: ID_A,
    twitchLogin: 'demo',
    liveChannelId: ID_B,
    modsRoleId: ID_C,
    levelsEnabled: 'off',
    somethingRemoved: 'ignored'
  };

  test('a blob naming this guild is the config to migrate', () => {
    const out = legacyConfigFor(legacy, ID_A);
    expect(out?.liveChannelId).toBe(ID_B);
    expect(out?.modsRoleId).toBe(ID_C);
    expect(out?.levelsEnabled).toBe('off');
    expect(Object.keys(out ?? {})).toEqual([...DISCORD_CONFIG_KEYS]);
  });

  test.each([
    ['a blob naming a different guild is not inherited', legacy, ID_B],
    ['an already narrowed blob has nothing to migrate', { guildId: ID_A, twitchLogin: 'demo' }, ID_A],
    ['a blob with no guild has nothing to migrate', { twitchLogin: 'demo' }, ID_A],
    ['a guild id that is not a snowflake never migrates', { ...legacy, guildId: 'abc' }, 'abc']
  ])('%s', (_name, blob, guildId) => {
    expect(legacyConfigFor(blob, guildId)).toBeNull();
  });

  test('a missing or malformed blob is not a migration', () => {
    for (const blob of [null, undefined, 'x', [legacy], 7]) {
      expect(legacyConfigFor(blob, ID_A)).toBeNull();
    }
  });
});

describe('dropped pins', () => {
  const ID_1 = '901234567890123456';
  const ID_2 = '789012345678901234';

  test('unknown slots, blanks and repeats are dropped and the order is fixed', () => {
    expect(droppedPinNotice(['mods', 'owner', 'mods', 'nosuchslot', '', 7, null])).toEqual([
      'owner',
      'mods'
    ]);
  });

  test('nothing dropped is an empty notice', () => {
    expect(droppedPinNotice([])).toEqual([]);
    expect(droppedPinNotice(undefined)).toEqual([]);
    expect(droppedPinNotice(null)).toEqual([]);
  });

  test('clearing a slot leaves every other pin alone', () => {
    const config = { ...blankDiscordConfig(), pinnedRoles: `owner=${ID_2},mods=${ID_1}` };
    expect(clearPinnedSlots(config, ['mods']).pinnedRoles).toBe(`owner=${ID_2}`);
  });

  test('clearing nothing returns the config untouched', () => {
    const config = { ...blankDiscordConfig(), pinnedRoles: `mods=${ID_1}` };
    expect(clearPinnedSlots(config, [])).toBe(config);
  });

  test('clearing a slot that was never pinned is not a change', () => {
    const config = { ...blankDiscordConfig(), pinnedRoles: `mods=${ID_1}` };
    expect(clearPinnedSlots(config, ['vip']).pinnedRoles).toBe(`mods=${ID_1}`);
  });
});

describe('refused fields', () => {
  test.each([
    ['known fields are marked and the first one is the one to name', ['modsRoleId', 'ticketPanelTitle'], { byField: { modsRoleId: true, ticketPanelTitle: true }, first: 'modsRoleId', unknown: [] }],
    ['a name this console has no field for falls back to the banner', ['gone_field', 'gone_field'], { byField: {}, first: '', unknown: ['gone_field'] }],
    ['a mixed list marks what it can and keeps the rest for the banner', ['gone_field', 'modsRoleId'], { byField: { modsRoleId: true }, first: 'modsRoleId', unknown: ['gone_field'] }],
    ['non-strings and blanks never reach either list', [7, null, undefined, '', '   ', { field: 'modsRoleId' }], { byField: {}, first: '', unknown: [] }],
    ['a padded name still names its own field', [' modsRoleId '], { byField: { modsRoleId: true }, first: 'modsRoleId', unknown: [] }],
    ['no refusal is an empty map', undefined, { byField: {}, first: '', unknown: [] }]
  ] as [string, readonly unknown[] | undefined, ReturnType<typeof fieldErrorsByField>][])('%s', (_name, refused, want) => {
    expect(fieldErrorsByField(refused)).toEqual(want);
  });
});

describe('the repost panel payload', () => {
  test('a colour the streamer never set is omitted, not defaulted on the wire', () => {
    const payload = ticketPanelPayload(blankDiscordConfig());
    expect('color' in payload).toBe(false);
    expect(hexToDiscordColor('')).toBeNull();
    expect(payload.title).toBe(TICKET_PANEL_DEFAULTS.title);
  });

  test('black is a colour, not an absence', () => {
    expect(ticketPanelPayload({ ...blankDiscordConfig(), ticketPanelColor: '#000000' }).color).toBe(0);
    expect(hexToDiscordColor('#000')).toBe(0);
    expect(hexToDiscordColor('#000000')).toBe(0);
  });

  test('a chosen colour travels as the decimal Go parses', () => {
    const payload = ticketPanelPayload({ ...blankDiscordConfig(), ticketPanelColor: LIVE_COLOR_HEX });
    expect(payload.color).toBe(0xc47a3a);
    expect(hexToDiscordColor('C47A3A')).toBe(0xc47a3a);
  });

  test('an unparsable colour is omitted, and the merge reports it as a field error', () => {
    const payload = ticketPanelPayload({ ...blankDiscordConfig(), ticketPanelColor: 'rebeccapurple' });
    expect('color' in payload).toBe(false);
    const { config, errors } = mergeDiscordConfig(blankDiscordConfig(), { ticketPanelColor: 'rebeccapurple' });
    expect(errors).toEqual([{ field: 'ticketPanelColor', code: 'color' }]);
    expect(config.ticketPanelColor).toBe('');
  });
});

describe('guild icons', () => {
  test('the picker keeps the hash Discord sent and builds the CDN url from it', () => {
    expect(parseUserGuilds([{ id: ID_A, name: 'x', icon: 'abc123' }, { id: ID_B, name: 'x', icon: null }])?.map((g) => g.icon)).toEqual(['abc123', '']);
    expect(guildIconURL(ID_A, 'abc123')).toBe(`https://cdn.discordapp.com/icons/${ID_A}/abc123.png`);
    expect(guildIconURL(ID_A, 'a_9f0')).toBe(`https://cdn.discordapp.com/icons/${ID_A}/a_9f0.png`);
  });

  test('no icon, a bad id, or a hash that is not one means no url', () => {
    expect(guildIconURL(ID_A, '')).toBe('');
    expect(guildIconURL('nope', 'abc')).toBe('');
    expect(guildIconURL(ID_A, '../x')).toBe('');
  });

  test('the size parameter is only asked of the CDN', () => {
    const url = guildIconURL(ID_A, 'abc');
    expect(guildIconSrc(url)).toBe(`${url}?size=128`);
    expect(guildIconSrc(url, 64)).toBe(`${url}?size=64`);
    expect(guildIconSrc('/logo.png')).toBe('/logo.png');
    expect(guildIconSrc('')).toBe('');
  });
});
