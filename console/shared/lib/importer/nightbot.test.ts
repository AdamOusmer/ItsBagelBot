// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Behaviour contract for the Nightbot parser. Unlike moobot.test.ts and
// streamelements.test.ts there is no Go implementation to pin against — no Go
// Nightbot parser ever existed — so these expectations ARE the contract:
// change one only when the mapping itself is meant to change.

import { describe, expect, test } from 'bun:test';
import { detectNightbot, NB_CODE, NightbotExportError, parseNightbot } from './nightbot';
import { CODE, isValidFetchDefName, validateManifest } from './validate';
import type { ImportDiagnostic } from './types';

const bytes = (doc: unknown): Uint8Array => new TextEncoder().encode(JSON.stringify(doc));

const codesOf = (diags: ImportDiagnostic[]): string[] => diags.map((d) => d.code);

const command = (over: Record<string, unknown> = {}): Record<string, unknown> => ({
  _id: 'abc',
  name: '!hello',
  message: 'Hi $(user)!',
  coolDown: 30,
  count: 0,
  userLevel: 'everyone',
  ...over
});

describe('detectNightbot', () => {
  test('accepts a saved /1/commands response', () => {
    expect(detectNightbot(bytes({ _total: 1, commands: [command()] }))).toBe(true);
  });

  test('accepts a bare array of command rows', () => {
    expect(detectNightbot(bytes([command()]))).toBe(true);
  });

  test('accepts a spam-protection save-out on its own', () => {
    expect(
      detectNightbot(bytes({ spam_protection: [{ type: 'blacklist', blacklist: ['badword'] }] }))
    ).toBe(true);
  });

  test('rejects a StreamElements envelope wearing the same key', () => {
    expect(detectNightbot(bytes({ commands: [{ command: '!hello', reply: 'hi' }] }))).toBe(false);
  });

  test('rejects junk', () => {
    expect(detectNightbot(new TextEncoder().encode('not json'))).toBe(false);
    expect(detectNightbot(bytes({ hello: 'world' }))).toBe(false);
  });
});

describe('envelope shapes', () => {
  test('bundle of raw API responses carries both collections', () => {
    const { manifest } = parseNightbot(
      bytes({
        commands: { _total: 1, commands: [command()] },
        timers: { _total: 1, timers: [{ name: 'promo', message: 'follow me', interval: 10 }] }
      })
    );
    expect(manifest.commands).toHaveLength(1);
    expect(manifest.timers).toHaveLength(1);
  });

  test('a file with nothing recognizable throws', () => {
    expect(() => parseNightbot(bytes({ hello: 'world' }))).toThrow(NightbotExportError);
    expect(() => parseNightbot(new TextEncoder().encode('{'))).toThrow(NightbotExportError);
  });
});

describe('commands', () => {
  test('name, cooldown and variables translate', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({
        commands: [
          command({ name: '!HELLO', message: 'Hi $(touser), welcome to $(channel) — $(query)', coolDown: 45 })
        ]
      })
    );
    expect(manifest.commands?.[0]).toEqual({
      name: 'hello',
      responses: ['Hi {touser}, welcome to {channel} — {args}'],
      permission: 'everyone',
      cooldown_seconds: 45
    });
    expect(diagnostics).toEqual([]);
  });

  test('user levels map onto perm tiers', () => {
    const levels = ['everyone', 'subscriber', 'twitch_vip', 'moderator', 'owner'];
    const { manifest } = parseNightbot(
      bytes({
        commands: levels.map((userLevel, i) => command({ name: `!c${i}`, userLevel, message: 'hi' }))
      })
    );
    expect(manifest.commands?.map((c) => c.permission)).toEqual([
      'everyone',
      'sub',
      'vip',
      'mod',
      'broadcaster'
    ]);
  });

  test('regular widens to everyone with a note', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ commands: [command({ userLevel: 'regular', message: 'hi' })] })
    );
    expect(manifest.commands?.[0].permission).toBe('everyone');
    expect(codesOf(diagnostics)).toEqual([NB_CODE.commandRegularWidened]);
    expect(manifest.commands?.[0].warnings).toHaveLength(1);
  });

  test('an unknown user level widens with permission_unmapped', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ commands: [command({ userLevel: 'founder', message: 'hi' })] })
    );
    expect(manifest.commands?.[0].permission).toBe('everyone');
    expect(codesOf(diagnostics)).toEqual([CODE.permissionUnmapped]);
  });

  test('unmappable variables stay literal and are reported once each', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({
        commands: [command({ message: '$(count) $(count) $(querystring) $(eval 1+1) $(1)' })]
      })
    );
    expect(manifest.commands?.[0].responses).toEqual([
      '$(count) $(count) $(querystring) $(eval 1+1) $(1)'
    ]);
    expect(codesOf(diagnostics)).toEqual([
      CODE.variableUnmapped,
      CODE.variableUnmapped,
      CODE.variableUnmapped,
      CODE.variableUnmapped
    ]);
  });

  test('a row without name/message is skipped, not fatal', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ commands: [{ _id: 'x' }, command({ name: '   ' }), command()] })
    );
    expect(manifest.commands).toHaveLength(1);
    expect(codesOf(diagnostics)).toEqual([NB_CODE.commandUnparseable, NB_CODE.commandNameInvalid]);
  });

  test('an empty response errors so commit skips it', () => {
    const { diagnostics } = parseNightbot(bytes({ commands: [command({ message: '   ' })] }));
    expect(diagnostics.filter((d) => d.severity === 'error').map((d) => d.code)).toEqual([
      CODE.responseInvalid
    ]);
  });
});

describe('urlfetch synthesis', () => {
  test('urlfetch and customapi become definitions with legal slugs', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({
        commands: [
          command({
            name: '!weather',
            message: '$(urlfetch https://api.example.com/w) / $(customapi https://api.example.com/x)'
          })
        ]
      })
    );
    expect(manifest.commands?.[0].responses).toEqual([
      '{urlfetch:nightbot_weather} / {urlfetch:nightbot_weather_2}'
    ]);
    expect(manifest.fetches).toEqual([
      { name: 'nightbot_weather', url: 'https://api.example.com/w', source: 'nightbot' },
      { name: 'nightbot_weather_2', url: 'https://api.example.com/x', source: 'nightbot' }
    ]);
    for (const f of manifest.fetches ?? []) expect(isValidFetchDefName(f.name)).toBe(true);
    expect(validateManifest(manifest).filter((d) => d.severity === 'error')).toEqual([]);
    expect(diagnostics).toEqual([]);
  });

  test('the same URL twice in one command shares its definition', () => {
    const { manifest } = parseNightbot(
      bytes({
        commands: [
          command({ name: '!x', message: '$(urlfetch https://a.example/1) $(urlfetch https://a.example/1)' })
        ]
      })
    );
    expect(manifest.fetches).toHaveLength(1);
    expect(manifest.commands?.[0].responses).toEqual(['{urlfetch:nightbot_x} {urlfetch:nightbot_x}']);
  });

  test('json mode maps but flags the missing path', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ commands: [command({ name: '!j', message: '$(urlfetch json https://a.example/j)' })] })
    );
    expect(manifest.fetches?.[0]).toEqual({
      name: 'nightbot_j',
      url: 'https://a.example/j',
      source: 'nightbot'
    });
    expect(codesOf(diagnostics)).toEqual([CODE.variableUnmapped]);
  });

  test('a URL built out of another variable is never baked into a definition', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ commands: [command({ name: '!q', message: '$(urlfetch https://a.example/?q=$(query))' })] })
    );
    expect(manifest.fetches).toBeUndefined();
    expect(manifest.commands?.[0].responses).toEqual(['$(urlfetch https://a.example/?q={args})']);
    expect(codesOf(diagnostics)).toEqual([CODE.variableUnmapped]);
  });

  test('a non-https URL is refused rather than synthesized dead', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ commands: [command({ name: '!f', message: '$(urlfetch ftp://a.example/x)' })] })
    );
    expect(manifest.fetches).toBeUndefined();
    expect(codesOf(diagnostics)).toEqual([CODE.variableUnmapped]);
  });
});

describe('timers', () => {
  test('minutes become seconds and the timer stays live-only', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ timers: [{ name: 'promo', message: 'follow $(channel)', interval: 15, lines: 0 }] })
    );
    expect(manifest.timers).toEqual([
      { message: 'follow {channel}', interval_seconds: 900, online_only: true }
    ]);
    expect(diagnostics).toEqual([]);
  });

  test('the chat-line gate is reported as dropped', () => {
    const { diagnostics } = parseNightbot(
      bytes({ timers: [{ name: 'promo', message: 'hi', interval: 5, lines: 20 }] })
    );
    expect(codesOf(diagnostics)).toEqual([NB_CODE.timerLinesIgnored]);
  });

  test('a disabled timer is skipped', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ timers: [{ name: 'off', message: 'hi', interval: 5, enabled: false }] })
    );
    expect(manifest.timers).toBeUndefined();
    expect(codesOf(diagnostics)).toEqual([NB_CODE.timerDisabledSkipped]);
  });

  test('timer variables translate but never synthesize a definition', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ timers: [{ name: 't', message: '$(urlfetch https://a.example/t)', interval: 5 }] })
    );
    expect(manifest.fetches).toBeUndefined();
    expect(codesOf(diagnostics)).toEqual([NB_CODE.timerVariableUnmapped]);
  });
});

describe('spam protection', () => {
  test('blacklist terms become automod block terms, regex entries skipped', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({
        spam_protection: [
          { type: 'links', enabled: true },
          { type: 'blacklist', blacklist: ['badword', 'BadWord', '~/spam.*/', '  '] }
        ]
      })
    );
    expect(manifest.automod).toEqual({ block: ['badword'] });
    expect(codesOf(diagnostics)).toEqual([NB_CODE.automodRegexSkipped]);
  });
});
