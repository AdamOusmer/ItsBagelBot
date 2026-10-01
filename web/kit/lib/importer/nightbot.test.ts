// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import {
  detectNightbot,
  fetchNightbot,
  NB_CODE,
  NightbotExportError,
  NightbotFetchError,
  parseNightbot
} from './nightbot';
import { CODE, isValidFetchDefName, validateManifest } from './validate';
import type { ImportDiagnostic } from './types';

const bytes = (doc: unknown): Uint8Array => new TextEncoder().encode(JSON.stringify(doc));
const TESTDATA = join(dirname(import.meta.path), 'testdata');

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
  const text = (value: string) => new TextEncoder().encode(value);

  test.each([
    ['accepts a saved /1/commands response', bytes({ _total: 1, commands: [command()] }), true],
    ['accepts a bare array of command rows', bytes([command()]), true],
    ['accepts a spam-protection save-out on its own', bytes({ spam_protection: [{ type: 'blacklist', blacklist: ['badword'] }] }), true],
    ['rejects a StreamElements envelope wearing the same key', bytes({ commands: [{ command: '!hello', reply: 'hi' }] }), false],
    ['rejects text that is not JSON', text('not json'), false],
    ['rejects an object with nothing recognizable', bytes({ hello: 'world' }), false]
  ] as [string, Uint8Array, boolean][])('%s', (_name, doc, want) => {
    expect(detectNightbot(doc)).toBe(want);
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
          command({ name: '!HELLO', message: 'Hi $(touser), welcome to $(channel), $(query)', coolDown: 45 })
        ]
      })
    );
    expect(manifest.commands?.[0]).toEqual({
      name: 'hello',
      responses: ['Hi {touser}, welcome to {channel}, {args}'],
      source_responses: ['Hi $(touser), welcome to $(channel), $(query)'],
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

interface MessageCase {
  name: string;
  messages: string[];
  responses: string[][];
  codes: string[];
}

const UNMAPPED = CODE.variableUnmapped;

const MESSAGE_CASES: MessageCase[] = [
  { name: 'unmappable variables stay literal and are reported once each', messages: ['$(eval 1+1) $(eval 1+1) $(twitch x) $(31)'], responses: [['$(eval 1+1) $(eval 1+1) $(twitch x) $(31)']], codes: [UNMAPPED, UNMAPPED, UNMAPPED] },
  { name: '$(count) translates to bare {count}, the per-command run count', messages: ['hugged $(count) times'], responses: [['hugged {count} times']], codes: [] },
  { name: '$(querystring) translates to {querystring}, not to {args}', messages: ['https://x.test/?q=$(querystring)'], responses: [['https://x.test/?q={querystring}']], codes: [] },
  { name: 'word numbers map onto the positional tokens', messages: ['$(1) hugs $(2), $(30) last'], responses: [['{1} hugs {2}, {30} last']], codes: [] },
  { name: 'a word number past the cap stays literal rather than becoming a dead span', messages: ['$(31) $(0) $(1x)'], responses: [['$(31) $(0) $(1x)']], codes: [UNMAPPED, UNMAPPED, UNMAPPED] },
  { name: '$(time tz) passes the timezone payload through', messages: ['it is $(time America/New_York) there'], responses: [['it is {time:America/New_York} there']], codes: [] },
  { name: '$(countdown d)/$(countup d) normalize a parseable date', messages: ['left: $(countdown Dec 25 2026 12:00:00 AM EST)', 'since: $(countup 2026-01-01)'], responses: [['left: {countdown:2026-12-25T05:00:00.000Z}'], ['since: {countup:2026-01-01}']], codes: [] },
  { name: '$(countdown d) warns on an unparsable or time-only date', messages: ['$(countdown whenever)', '$(countdown 5:00:00 PM EST)'], responses: [['$(countdown whenever)'], ['$(countdown 5:00:00 PM EST)']], codes: [UNMAPPED, UNMAPPED] },
  { name: '$(twitch $(channel) "{{title}}") maps the single field, own channel', messages: ['now: $(twitch $(channel) "{{title}}")'], responses: [['now: {title}']], codes: [] },
  {
    name: '$(twitch $(channel) "{{game}}")/{{uptimeLength}}/{{viewers}}/{{followers}}/{{subscriberCount}}',
    messages: ['game', 'uptimeLength', 'viewers', 'followers', 'subscriberCount'].map((field) => `$(twitch $(channel) "{{${field}}}")`),
    responses: [['{game}'], ['{uptime}'], ['{channel.viewers}'], ['{followers}'], ['{subs}']],
    codes: []
  },
  { name: '$(twitch bob "{{title}}") names another channel', messages: ['title', 'game', 'uptimeLength'].map((field) => `$(twitch bob "{{${field}}}")`), responses: [['{title:bob}'], ['{game:bob}'], ['{uptime:bob}']], codes: [] },
  { name: '$(twitch bob "{{viewers}}") has no other-channel form and stays unmapped', messages: ['$(twitch bob "{{viewers}}")'], responses: [['{{viewers}}']], codes: [UNMAPPED] },
  { name: 'a mixed format string keeps its text, maps what it can, warns on the rest', messages: ['$(twitch $(channel) "{{displayName}} is playing {{game}}")'], responses: [['{{displayName}} is playing {game}']], codes: [UNMAPPED] },
  { name: 'stray braces around a mapped field refuse the whole call rather than mint a broken span', messages: ['$(twitch $(channel) "{oops {{game}}")'], responses: [['$(twitch {channel} "{oops {{game}}")']], codes: [UNMAPPED] }
];

describe('message variables', () => {
  test.each(MESSAGE_CASES)('$name', ({ messages, responses, codes }) => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ commands: messages.map((message, i) => command({ name: `!c${i}`, message })) })
    );
    expect(manifest.commands?.map((c) => c.responses)).toEqual(responses);
    expect(codesOf(diagnostics)).toEqual(codes);
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
    expect(codesOf(diagnostics)).toEqual(['fetch_def_created', 'fetch_def_created']);
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
    expect(codesOf(diagnostics)).toEqual(['fetch_def_created', CODE.variableUnmapped]);
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

  test('lines becomes a 5 minute chat gate', () => {
    const { manifest, diagnostics } = parseNightbot(
      bytes({ timers: [{ name: 'promo', message: 'hi', interval: 5, lines: 20 }] })
    );
    expect(manifest.timers).toEqual([
      { message: 'hi', interval_seconds: 300, online_only: true, min_chat_lines: 20, chat_window_minutes: 5 }
    ]);
    expect(diagnostics).toEqual([]);
  });

  test('a missing or non-positive lines value adds no gate', () => {
    const { manifest } = parseNightbot(
      bytes({
        timers: [
          { name: 'a', message: 'hi', interval: 5 },
          { name: 'b', message: 'hi', interval: 5, lines: -3 }
        ]
      })
    );
    for (const t of manifest.timers!) {
      expect(t.min_chat_lines).toBeUndefined();
      expect(t.chat_window_minutes).toBeUndefined();
    }
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
  test.each([
    ['live-API filter shape: newline-delimited blacklist string under _type', [
      { _type: 'links', enabled: true },
      { _type: 'blacklist', enabled: true, blacklist: 'badword\nBadWord\n~/spam.*/\n  ' }
    ]],
    ['blacklist terms become automod block terms, regex entries skipped', [
      { type: 'links', enabled: true },
      { type: 'blacklist', blacklist: ['badword', 'BadWord', '~/spam.*/', '  '] }
    ]]
  ])('%s', (_name, spam_protection) => {
    const { manifest, diagnostics } = parseNightbot(bytes({ spam_protection }));
    expect(manifest.automod).toEqual({ block: ['badword'] });
    expect(codesOf(diagnostics)).toEqual([NB_CODE.automodRegexSkipped]);
  });
});

interface Recorded {
  path: string;
  authorization: string | null;
  accept: string | null;
}
async function withTestServer(
  handler: (req: Request) => Response | Promise<Response>,
  run: (baseUrl: string, requests: Recorded[]) => Promise<void>
): Promise<void> {
  const requests: Recorded[] = [];
  const server = Bun.serve({
    port: 0,
    async fetch(req) {
      requests.push({
        path: new URL(req.url).pathname,
        authorization: req.headers.get('authorization'),
        accept: req.headers.get('accept')
      });
      return handler(req);
    }
  });
  try {
    await run(`http://127.0.0.1:${server.port}`, requests);
  } finally {
    server.stop(true);
  }
}

const TEST_TOKEN = 'nb-test-access-token';

describe('fetch flow', () => {
  test('staples commands+timers+spam filters over three Bearer calls, parseable as-is', async () => {
    await withTestServer(
      (req) => {
        const path = new URL(req.url).pathname;
        if (path === '/1/commands')
          return Response.json({ _total: 1, status: 200, commands: [command()] });
        if (path === '/1/timers')
          return Response.json({
            _total: 1,
            status: 200,
            timers: [{ _id: 't1', name: 'plug', message: 'follow me', interval: '*/15 * * * *', enabled: true }]
          });
        if (path === '/1/spam_protection')
          return Response.json({
            status: 200,
            filters: [{ _type: 'blacklist', enabled: true, blacklist: 'badword\nother' }]
          });
        return new Response('not found', { status: 404 });
      },
      async (baseUrl, requests) => {
        const env = await fetchNightbot(TEST_TOKEN, { baseUrl });
        expect(requests.map((r) => r.path)).toEqual(['/1/commands', '/1/timers', '/1/spam_protection']);
        for (const r of requests) {
          expect(r.authorization).toBe(`Bearer ${TEST_TOKEN}`);
          expect(r.accept).toContain('application/json');
        }

        const { manifest } = parseNightbot(bytes(env));
        expect(manifest.commands).toHaveLength(1);
        expect(manifest.timers).toHaveLength(1);
        expect(manifest.automod).toEqual({ block: ['badword', 'other'] });
      }
    );
  });

  test('missing or malformed token refuses before any transport', async () => {
    await withTestServer(
      () => Response.json({}),
      async (baseUrl, requests) => {
        await expect(fetchNightbot('   ', { baseUrl })).rejects.toThrow(/access token is required/);
        await expect(fetchNightbot('has spaces inside', { baseUrl })).rejects.toThrow(/malformed/);
        expect(requests).toHaveLength(0);
      }
    );
  });

  test('401 carries endpoint, status and reconnect remediation', async () => {
    await withTestServer(
      () => new Response('{"status":401,"message":"invalid token"}', { status: 401 }),
      async (baseUrl) => {
        const err = await fetchNightbot(TEST_TOKEN, { baseUrl }).catch((e) => e as Error);
        expect(err).toBeInstanceOf(NightbotFetchError);
        expect(err.message).toContain('/1/commands returned 401');
        expect(err.message).toContain('reconnect your Nightbot account');
      }
    );
  });

  test('spam_protection failure degrades to no blacklist instead of failing the fetch', async () => {
    await withTestServer(
      (req) => {
        const path = new URL(req.url).pathname;
        if (path === '/1/spam_protection') return new Response('boom', { status: 500 });
        if (path === '/1/commands')
          return Response.json({ _total: 1, status: 200, commands: [command()] });
        return Response.json({ _total: 0, status: 200, timers: [] });
      },
      async (baseUrl) => {
        const env = await fetchNightbot(TEST_TOKEN, { baseUrl });
        expect(env.spam_protection).toEqual([]);
        expect(parseNightbot(bytes(env)).manifest.commands).toHaveLength(1);
      }
    );
  });

  test('malformed upstream JSON fails descriptively', async () => {
    await withTestServer(
      () => new Response('not json'),
      async (baseUrl) => {
        const err = await fetchNightbot(TEST_TOKEN, { baseUrl }).catch((e) => e as Error);
        expect(err.message).toContain('/1/commands');
        expect(err.message).toContain('decoding response');
      }
    );
  });
});

const NIGHTBOT_GOLDEN_ENVELOPE = {
  commands: [
    command({ name: '!hello', message: 'Hi $(touser), welcome to $(channel), $(query)', coolDown: 45 }),
    command({ name: '!hug', message: '$(1) hugs $(2), $(30) last', userLevel: 'subscriber' }),
    command({ name: '!count', message: 'hugged $(count) times' }),
    command({
      name: '!weather',
      message: '$(urlfetch https://api.example.com/w) / $(customapi https://api.example.com/x)'
    }),
    command({ name: '!unmapped', message: '$(eval 1+1) $(twitch x)' })
  ],
  timers: [{ name: 'promo', message: 'follow $(channel)', interval: 15, lines: 0 }]
};

describe('golden', () => {
  test('the representative envelope translates to the committed golden', () => {
    const { manifest, diagnostics } = parseNightbot(bytes(NIGHTBOT_GOLDEN_ENVELOPE));
    const golden = JSON.parse(readFileSync(join(TESTDATA, 'nightbot-golden.json'), 'utf8'));
    expect(manifest).toEqual(golden.manifest);
    expect(diagnostics).toEqual(golden.diagnostics);
  });
});
