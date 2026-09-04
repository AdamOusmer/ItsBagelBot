// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import {
  CATEGORY_NAME_MAX,
  DISCORD_CONFIG_KEYS,
  LIVE_COLOR_HEX,
  PINNED_SLOTS,
  TICKET_OPEN_LIMIT_DEFAULT,
  TICKET_PANEL_BODY_MAX,
  TICKET_PANEL_DEFAULTS,
  blankDiscordConfig,
  encodeIdList,
  encodeNameList,
  encodePinnedRoles,
  isHexColor,
  isSnowflake,
  mergeDiscordConfig,
  normalizeHex,
  parseDiscordConfig,
  parseIdList,
  parseNameList,
  parsePinnedRoles,
  pinnedRole,
  ticketLogChannel,
  ticketOpenLimitN,
  ticketPanelSpec,
  ticketStaffRoleIds,
  validateDiscordConfig
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

describe('validation', () => {
  test('a blank config is valid', () => {
    expect(validateDiscordConfig(blankDiscordConfig())).toEqual([]);
  });

  test('validation reports every bad field, not just the first', () => {
    const bad = {
      ...blankDiscordConfig(),
      guildId: 'abc',
      ticketPanelColor: 'nope',
      ticketOpenLimit: '9'
    };
    expect(validateDiscordConfig(bad)).toEqual([
      { field: 'guildId', code: 'snowflake' },
      { field: 'ticketOpenLimit', code: 'range' },
      { field: 'ticketPanelColor', code: 'color' }
    ]);
  });

  test('a pinned-roles string with an unknown slot is invalid', () => {
    const bad = { ...blankDiscordConfig(), pinnedRoles: `founder=${ID_A}` };
    expect(validateDiscordConfig(bad)).toEqual([{ field: 'pinnedRoles', code: 'pinned' }]);
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
