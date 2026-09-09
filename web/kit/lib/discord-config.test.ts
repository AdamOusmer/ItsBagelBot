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
  canManageGuild,
  clearPinnedSlots,
  droppedPinNotice,
  fieldErrorsByField,
  encodeIdList,
  encodeNameList,
  encodePinnedRoles,
  guildBotState,
  guildMonogram,
  guildPermissionBits,
  guildPickerBadge,
  legacyConfigFor,
  isHexColor,
  isSnowflake,
  mergeDiscordConfig,
  normalizeHex,
  parseConfigVersion,
  parseDiscordConfig,
  parseIdList,
  parseUserGuild,
  parseUserGuilds,
  parseNameList,
  parsePinnedRoles,
  pinnedRole,
  hexToDiscordColor,
  ticketLogChannel,
  ticketOpenLimitN,
  ticketPanelPayload,
  ticketPanelSpec,
  ticketStaffRoleIds
} from './discord-config';

const ID_A = '123456789012345678';
const ID_B = '234567890123456789';
const ID_C = '345678901234567890';

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
  test('a field the draft omits keeps its stored value', () => {
    const current = { ...blankDiscordConfig(), liveChannelId: ID_A };
    const { config } = mergeDiscordConfig(current, { clipsChannelId: ID_B });
    expect(config.liveChannelId).toBe(ID_A);
    expect(config.clipsChannelId).toBe(ID_B);
  });

  test('a bad snowflake keeps the stored value and reports the field', () => {
    const current = { ...blankDiscordConfig(), liveChannelId: ID_A };
    const { config, errors } = mergeDiscordConfig(current, { liveChannelId: '12' });
    expect(config.liveChannelId).toBe(ID_A);
    expect(errors).toEqual([{ field: 'liveChannelId', code: 'snowflake' }]);
  });

  test('an empty string clears a field', () => {
    const current = { ...blankDiscordConfig(), liveChannelId: ID_A };
    const { config, errors } = mergeDiscordConfig(current, { liveChannelId: '' });
    expect(config.liveChannelId).toBe('');
    expect(errors).toEqual([]);
  });

  test('non-string draft values are ignored, not coerced', () => {
    const current = { ...blankDiscordConfig(), ticketOpenLimit: '3' };
    const { config, errors } = mergeDiscordConfig(current, { ticketOpenLimit: 5 });
    expect(config.ticketOpenLimit).toBe('3');
    expect(errors).toEqual([]);
  });

  test('lists and colours are canonicalised on the way in', () => {
    const { config } = mergeDiscordConfig(blankDiscordConfig(), {
      ticketStaffRoleIds: ` ${ID_A} , ${ID_B} , ${ID_A} `,
      ticketPanelColor: 'C47A3A',
      pinnedRoles: ` vip = ${ID_B} , owner = ${ID_A} `
    });
    expect(config.ticketStaffRoleIds).toBe(`${ID_A},${ID_B}`);
    expect(config.ticketPanelColor).toBe(LIVE_COLOR_HEX);
    // Encoded in PINNED_SLOTS order, not submission order.
    expect(config.pinnedRoles).toBe(`owner=${ID_A},vip=${ID_B}`);
  });

  test('a malformed role id in a list is refused, not silently dropped', () => {
    const current = { ...blankDiscordConfig(), ticketStaffRoleIds: ID_A };
    const { config, errors } = mergeDiscordConfig(current, { ticketStaffRoleIds: `${ID_B},notasnowflake` });
    // The encoder would have dropped the bad half and stored ID_B, which reads
    // to the streamer as "one of my two picks vanished for no reason".
    expect(config.ticketStaffRoleIds).toBe(ID_A);
    expect(errors).toEqual([{ field: 'ticketStaffRoleIds', code: 'list' }]);
  });

  test('an unknown pin slot is refused, not silently dropped', () => {
    const { config, errors } = mergeDiscordConfig(blankDiscordConfig(), {
      pinnedRoles: `nosuchslot=${ID_A}`
    });
    expect(config.pinnedRoles).toBe('');
    expect(errors).toEqual([{ field: 'pinnedRoles', code: 'pinned' }]);
  });

  test('a malformed role id in a pin pair is refused', () => {
    const { errors } = mergeDiscordConfig(blankDiscordConfig(), { pinnedRoles: 'mods=notasnowflake' });
    expect(errors).toEqual([{ field: 'pinnedRoles', code: 'pinned' }]);
  });

  test('a colour that is not a colour is refused rather than swapped', () => {
    const current = { ...blankDiscordConfig(), ticketPanelColor: '#52b788' };
    const { config, errors } = mergeDiscordConfig(current, { ticketPanelColor: 'rebeccapurple' });
    expect(config.ticketPanelColor).toBe('#52b788');
    expect(errors).toEqual([{ field: 'ticketPanelColor', code: 'color' }]);
  });

  test('the colour shorthand still normalises', () => {
    const { config, errors } = mergeDiscordConfig(blankDiscordConfig(), { ticketPanelColor: 'ABC' });
    expect(config.ticketPanelColor).toBe('#aabbcc');
    expect(errors).toEqual([]);
  });

  test('a flag only accepts on/off', () => {
    const { config, errors } = mergeDiscordConfig(blankDiscordConfig(), { levelsEnabled: 'yes' });
    expect(config.levelsEnabled).toBe('');
    expect(errors).toEqual([{ field: 'levelsEnabled', code: 'flag' }]);
  });

  test('an over-long panel body is refused, not truncated', () => {
    const body = 'x'.repeat(TICKET_PANEL_BODY_MAX + 1);
    const { config, errors } = mergeDiscordConfig(blankDiscordConfig(), { ticketPanelBody: body });
    expect(config.ticketPanelBody).toBe('');
    expect(errors).toEqual([{ field: 'ticketPanelBody', code: 'length' }]);
  });

  test('the open limit is refused outside 1..5', () => {
    for (const bad of ['0', '6', '-1', 'two', '3.5']) {
      const { errors } = mergeDiscordConfig(blankDiscordConfig(), { ticketOpenLimit: bad });
      expect(errors).toEqual([{ field: 'ticketOpenLimit', code: 'range' }]);
    }
    expect(mergeDiscordConfig(blankDiscordConfig(), { ticketOpenLimit: '5' }).errors).toEqual([]);
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

  test('pinnedRole reads one slot out of the blob', () => {
    const config = { ...blankDiscordConfig(), pinnedRoles: `mods=${ID_C}` };
    expect(pinnedRole(config, 'mods')).toBe(ID_C);
    expect(pinnedRole(config, 'owner')).toBe('');
  });
});

describe('id lists', () => {
  test('parse trims, drops junk and dedupes; encode round-trips', () => {
    expect(parseIdList(` ${ID_A} , nope , ${ID_B} , ${ID_A} `)).toEqual([ID_A, ID_B]);
    expect(encodeIdList([ID_A, '12', ID_B])).toBe(`${ID_A},${ID_B}`);
    expect(encodeIdList([])).toBe('');
  });

  test('isSnowflake pins the 17-20 digit window', () => {
    expect(isSnowflake('1'.repeat(16))).toBe(false);
    expect(isSnowflake('1'.repeat(17))).toBe(true);
    expect(isSnowflake('1'.repeat(20))).toBe(true);
    expect(isSnowflake('1'.repeat(21))).toBe(false);
    expect(isSnowflake('1234567890123456a')).toBe(false);
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
      expect(isHexColor(bad)).toBe(false);
    }
    expect(normalizeHex('nope', '#000000')).toBe('#000000');
  });

  test('isHexColor accepts only the stored 6-digit shape', () => {
    expect(isHexColor('#c47a3a')).toBe(true);
    expect(isHexColor('#C47A3A')).toBe(true);
    expect(isHexColor('#abc')).toBe(false);
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

  test('the ticket log channel falls back to the staff log channel', () => {
    const base = { ...blankDiscordConfig(), logChannelId: ID_A };
    expect(ticketLogChannel(base)).toBe(ID_A);
    expect(ticketLogChannel({ ...base, ticketLogChannelId: ID_B })).toBe(ID_B);
  });
});

describe('config version', () => {
  test('a number, a quoted number and nothing all decode', () => {
    expect(parseConfigVersion(7)).toBe(7);
    expect(parseConfigVersion('7')).toBe(7);
    expect(parseConfigVersion(' 12 ')).toBe(12);
    expect(parseConfigVersion(undefined)).toBe(DISCORD_CONFIG_VERSION_NEW);
  });

  test('a value that is not a whole non-negative version reads as never-saved', () => {
    expect(parseConfigVersion(-1)).toBe(DISCORD_CONFIG_VERSION_NEW);
    expect(parseConfigVersion(1.5)).toBe(DISCORD_CONFIG_VERSION_NEW);
    expect(parseConfigVersion('abc')).toBe(DISCORD_CONFIG_VERSION_NEW);
    expect(parseConfigVersion(null)).toBe(DISCORD_CONFIG_VERSION_NEW);
    expect(parseConfigVersion(Number.MAX_SAFE_INTEGER + 2)).toBe(DISCORD_CONFIG_VERSION_NEW);
  });
});

describe('guild permissions', () => {
  test('ADMINISTRATOR and MANAGE_GUILD each qualify on their own', () => {
    expect(canManageGuild({ permissions: '8' })).toBe(true);
    expect(canManageGuild({ permissions: '32' })).toBe(true);
    expect(canManageGuild({ permissions: '40' })).toBe(true);
  });

  test('a guild with neither bit is filtered out', () => {
    // SEND_MESSAGES | VIEW_CHANNEL | ADD_REACTIONS: a normal member.
    expect(canManageGuild({ permissions: '3136' })).toBe(false);
    expect(canManageGuild({ permissions: '0' })).toBe(false);
    expect(canManageGuild({})).toBe(false);
  });

  test('owner qualifies whatever the bitfield says', () => {
    expect(canManageGuild({ owner: true, permissions: '0' })).toBe(true);
    expect(canManageGuild({ owner: false, permissions: '0' })).toBe(false);
  });

  test('a bitfield past 2^53 keeps every low bit', () => {
    // 1 << 50 (USE_EXTERNAL_APPS) plus MANAGE_GUILD. Number.parseInt on this
    // string rounds and the AND against 0x20 can come out zero, which is the
    // whole reason the parse is BigInt.
    const bits = (1n << 50n) | DISCORD_MANAGE_GUILD;
    expect(canManageGuild({ permissions: bits.toString() })).toBe(true);
    expect(guildPermissionBits(bits.toString())).toBe(bits);
    // The same field WITHOUT either management bit must still be refused.
    expect(canManageGuild({ permissions: (1n << 50n).toString() })).toBe(false);
  });

  test('a malformed bitfield is worth no permission at all', () => {
    expect(guildPermissionBits('12x')).toBe(0n);
    expect(guildPermissionBits('-8')).toBe(0n);
    expect(guildPermissionBits(undefined)).toBe(0n);
    expect(guildPermissionBits(8)).toBe(8n);
    expect(guildPermissionBits(1.5)).toBe(0n);
  });
});

describe('guild presentation', () => {
  test('the monogram takes two initials, or two letters from one word', () => {
    expect(guildMonogram('Demo Bakery')).toBe('DB');
    expect(guildMonogram('  the  bagel  house ')).toBe('TB');
    expect(guildMonogram('Bagels')).toBe('BA');
    expect(guildMonogram('x')).toBe('X');
    expect(guildMonogram('   ')).toBe('?');
  });

  test('reauth outranks offline on the bot pill', () => {
    expect(guildBotState({ botPresent: true })).toBe('online');
    expect(guildBotState({ botPresent: false })).toBe('offline');
    expect(guildBotState({ botPresent: false, needsReauth: true })).toBe('reauth');
    expect(guildBotState({ botPresent: true, needsReauth: true })).toBe('reauth');
    expect(guildBotState({})).toBe('offline');
  });

  test('an unread reauth flag is neutral, never green and never red', () => {
    // The listing reports needsReauth false both for a healthy grant and for
    // a lookup that never ran, so reauthUnknown has to beat botPresent in
    // BOTH directions or the pill asserts something nobody checked.
    expect(guildBotState({ botPresent: true, reauthUnknown: true })).toBe('unknown');
    expect(guildBotState({ botPresent: false, reauthUnknown: true })).toBe('unknown');
    // A flag that WAS read and came back true still wins: that one is known.
    expect(guildBotState({ botPresent: true, needsReauth: true, reauthUnknown: true })).toBe('reauth');
    expect(guildBotState({ botPresent: true, reauthUnknown: false })).toBe('online');
  });

  test('the picker badge separates my servers from someone else\'s', () => {
    const bound = [ID_A, ID_B];
    expect(guildPickerBadge(ID_A, { bound })).toBe('mine');
    expect(guildPickerBadge(ID_C, { bound })).toBe('addable');
    expect(guildPickerBadge(ID_A, { bound: [] })).toBe('addable');
    expect(guildPickerBadge(ID_C, { bound, elsewhere: [ID_C] })).toBe('elsewhere');
    // A guild in both lists is mine: my own binding is the stronger fact.
    expect(guildPickerBadge(ID_A, { bound, elsewhere: [ID_A] })).toBe('mine');
  });
});

describe('parseUserGuilds', () => {
  test('keeps the permission bitfield as the string Discord sent', () => {
    const guilds = parseUserGuilds([
      { id: ID_A, name: 'Demo Bakery', owner: true, permissions: '1125899906842623' }
    ]);
    expect(guilds).toEqual([
      { id: ID_A, name: 'Demo Bakery', owner: true, permissions: '1125899906842623' }
    ]);
  });

  test('a 429 body is not an empty guild list', () => {
    expect(parseUserGuilds({ message: 'You are being rate limited.', retry_after: 1.5 })).toBeNull();
  });

  test('an HTML error page is not an empty guild list', () => {
    expect(parseUserGuilds('<!DOCTYPE html><html><body>502</body></html>')).toBeNull();
  });

  test('every non-array body is refused rather than emptied', () => {
    for (const body of [null, undefined, 0, '', {}, { guilds: [] }]) {
      expect(parseUserGuilds(body)).toBeNull();
    }
  });

  test('junk entries are dropped, good ones survive', () => {
    const guilds = parseUserGuilds([null, 'x', {}, { id: '' }, { id: ID_B }, [ID_C]]);
    expect(guilds).toEqual([{ id: ID_B, name: '', owner: false, permissions: '' }]);
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
    // Unknown keys never reach the guild row.
    expect(Object.keys(out ?? {})).toEqual([...DISCORD_CONFIG_KEYS]);
  });

  test('a blob naming a different guild is not inherited', () => {
    expect(legacyConfigFor(legacy, ID_B)).toBeNull();
  });

  test('an already narrowed blob has nothing to migrate', () => {
    expect(legacyConfigFor({ guildId: ID_A, twitchLogin: 'demo' }, ID_A)).toBeNull();
    expect(legacyConfigFor({ twitchLogin: 'demo' }, ID_A)).toBeNull();
  });

  test('a missing or malformed blob is not a migration', () => {
    for (const blob of [null, undefined, 'x', [legacy], 7]) {
      expect(legacyConfigFor(blob, ID_A)).toBeNull();
    }
  });

  test('a guild id that is not a snowflake never migrates', () => {
    expect(legacyConfigFor({ ...legacy, guildId: 'abc' }, 'abc')).toBeNull();
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
  test('known fields are marked and the first one is the one to name', () => {
    const out = fieldErrorsByField(['modsRoleId', 'ticketPanelTitle']);
    expect(out.byField).toEqual({ modsRoleId: true, ticketPanelTitle: true });
    expect(out.first).toBe('modsRoleId');
    expect(out.unknown).toEqual([]);
  });

  test('a name this console has no field for falls back to the banner', () => {
    const out = fieldErrorsByField(['gone_field', 'gone_field']);
    expect(out.byField).toEqual({});
    expect(out.first).toBe('');
    expect(out.unknown).toEqual(['gone_field']);
  });

  test('a mixed list marks what it can and keeps the rest for the banner', () => {
    const out = fieldErrorsByField(['gone_field', 'modsRoleId']);
    expect(out.byField).toEqual({ modsRoleId: true });
    expect(out.first).toBe('modsRoleId');
    expect(out.unknown).toEqual(['gone_field']);
  });

  test('non-strings and blanks never reach either list', () => {
    const out = fieldErrorsByField([7, null, undefined, '', '   ', { field: 'modsRoleId' }]);
    expect(out.byField).toEqual({});
    expect(out.unknown).toEqual([]);
  });

  test('a padded name still names its own field', () => {
    expect(fieldErrorsByField([' modsRoleId ']).first).toBe('modsRoleId');
  });

  test('no refusal is an empty map', () => {
    expect(fieldErrorsByField(undefined)).toEqual({ byField: {}, first: '', unknown: [] });
  });
});

describe('the repost panel payload', () => {
  test('a colour the streamer never set is omitted, not defaulted on the wire', () => {
    // Absent key, not 0 and not the brand hex: outgress reads absent as
    // "paint it the brand colour", so sending one freezes today's amber into
    // every panel and sending 0 would paint them black.
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
