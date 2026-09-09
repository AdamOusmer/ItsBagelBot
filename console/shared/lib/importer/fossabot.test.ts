// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Behaviour contract for the Fossabot parser. As with nightbot.test.ts there
// is no Go implementation to pin against, so these expectations ARE the
// contract: change one only when the mapping itself is meant to change.
//
// testdata/fossabot-golden.json pins the whole translation of
// testdata/fossabot-fixture.json (20 rows captured verbatim from the public
// myth directory on 2026-09-07, plus three rows with 9999-prefixed ids for
// cases that feed happens not to contain: a command disabled both online and
// offline, a role id the roles table does not carry, and a $(customapi …) with
// a URL next to an alias that cannot be a command name here). The golden was
// generated once, read line by line, and committed: regenerating it from the
// code under test would only prove the code equals itself, so this file has no
// regeneration path. When a mapping changes on purpose, write the new
// expectation by hand.

import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  detectFossabot,
  FB_CODE,
  fetchFossabot,
  FossabotExportError,
  FossabotFetchError,
  parseFossabot
} from './fossabot';
import { CODE, isValidFetchDefName, validateManifest } from './validate';
import type { ImportDiagnostic } from './types';

const here = dirname(fileURLToPath(import.meta.url));

const bytes = (doc: unknown): Uint8Array => new TextEncoder().encode(JSON.stringify(doc));

const codesOf = (diags: ImportDiagnostic[]): string[] => diags.map((d) => d.code);

// command builds one directory row with the fields the live feed carries.
const command = (over: Record<string, unknown> = {}): Record<string, unknown> => ({
  id: '90019',
  name: 'hello',
  response: 'Hi $(user)!',
  type: 'custom',
  aliases: [],
  role_ids: [],
  enabled_online: true,
  enabled_offline: true,
  ...over
});

// ROLES is the role table of the fixture channel, reused by the unit cases so
// role ids read the same everywhere.
const ROLES = [
  { id: '242749', name: 'Moderator', default: true },
  { id: '242755', name: '[Imported] Channel Editor', default: false },
  { id: '242753', name: 'Broadcaster', default: true }
];

const feed = (commands: Record<string, unknown>[]): Uint8Array =>
  bytes({ channel: { id: '30602', login: 'myth' }, roles: ROLES, commands });

describe('detectFossabot', () => {
  test('accepts the stapled fetch envelope', () => {
    expect(detectFossabot(feed([command()]))).toBe(true);
  });

  test('accepts a bare array of rows and a lone commands key', () => {
    expect(detectFossabot(bytes([command()]))).toBe(true);
    expect(detectFossabot(bytes({ commands: [command()] }))).toBe(true);
  });

  test('rejects a Nightbot envelope wearing the same key', () => {
    expect(detectFossabot(bytes({ commands: [{ name: '!hello', message: 'hi' }] }))).toBe(false);
  });

  test('rejects junk', () => {
    expect(detectFossabot(new TextEncoder().encode('not json'))).toBe(false);
    expect(detectFossabot(bytes({ hello: 'world' }))).toBe(false);
  });

  test('a feed with nothing recognizable throws from the parser', () => {
    expect(() => parseFossabot(bytes({ hello: 'world' }))).toThrow(FossabotExportError);
    expect(() => parseFossabot(new TextEncoder().encode('{'))).toThrow(FossabotExportError);
  });
});

describe('commands', () => {
  test('name, aliases and variables translate', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([
        command({
          name: 'Hello',
          aliases: ['HI', 'hello'],
          response: 'Hi $(touser), welcome to $(channel): $(query)'
        })
      ])
    );
    expect(manifest.commands?.[0]).toEqual({
      name: 'hello',
      responses: ['Hi {touser}, welcome to {channel}: {args}'],
      permission: 'everyone',
      aliases: ['hi']
    });
    expect(diagnostics).toEqual([]);
  });

  test('the user subfields map onto the identity tokens', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ response: '$(user.id) / $(user.login) / $(user)' })])
    );
    expect(manifest.commands?.[0].responses).toEqual(['{userid} / {user.login} / {user}']);
    expect(codesOf(diagnostics)).toEqual([]);
  });

  test('word numbers map onto the positional tokens', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ response: '$(1) hugs $(2)' })])
    );
    expect(manifest.commands?.[0].responses).toEqual(['{1} hugs {2}']);
    expect(codesOf(diagnostics)).toEqual([]);
  });

  test('$(sender) and $(user) both mean the caller', () => {
    const { manifest } = parseFossabot(feed([command({ response: '$(sender) / $(user)' })]));
    expect(manifest.commands?.[0].responses).toEqual(['{user} / {user}']);
  });

  test('unmappable variables stay literal and are reported once each', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ response: '$(uptime) $(uptime) $(time America/Los_Angeles) $(count.increment died 1)' })])
    );
    expect(manifest.commands?.[0].responses).toEqual([
      '$(uptime) $(uptime) $(time America/Los_Angeles) $(count.increment died 1)'
    ]);
    expect(codesOf(diagnostics)).toEqual([
      CODE.variableUnmapped,
      CODE.variableUnmapped,
      CODE.variableUnmapped
    ]);
  });

  test('a chat-action prefix is dropped and the text kept', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ response: '.me waves at $(user)' })])
    );
    expect(manifest.commands?.[0].responses).toEqual(['waves at {user}']);
    expect(codesOf(diagnostics)).toEqual([FB_CODE.commandActionPrefixDropped]);
  });

  test('a built-in command is skipped, not imported as a description', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ type: 'default', name: 'dadjoke', response: 'Tells an unfunny dad joke.' }), command()])
    );
    expect(manifest.commands).toHaveLength(1);
    expect(diagnostics[0]).toMatchObject({ item_index: -1, code: FB_CODE.commandBuiltinSkipped });
  });

  test('a command disabled online and offline is skipped', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ enabled_online: false, enabled_offline: false }), command({ name: 'other' })])
    );
    expect(manifest.commands?.map((c) => c.name)).toEqual(['other']);
    expect(diagnostics).toEqual([
      { severity: 'warn', item_index: -1, code: FB_CODE.commandDisabled, message: expect.any(String) }
    ]);
  });

  test('live-only carries online_only, offline-only comes over always on', () => {
    const { manifest } = parseFossabot(
      feed([
        command({ name: 'live', enabled_offline: false }),
        command({ name: 'offline', enabled_online: false })
      ])
    );
    expect(manifest.commands?.[0]).toMatchObject({ name: 'live', online_only: true });
    expect(manifest.commands?.[1].online_only).toBeUndefined();
  });

  test('an alias that cannot be a command name is dropped with a note', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ aliases: ['ok', 'bad alias'] })])
    );
    expect(manifest.commands?.[0].aliases).toEqual(['ok']);
    expect(codesOf(diagnostics)).toEqual([CODE.aliasInvalid]);
  });

  test('an empty response errors so commit skips it', () => {
    const { diagnostics } = parseFossabot(feed([command({ response: '   ' })]));
    expect(diagnostics.filter((d) => d.severity === 'error').map((d) => d.code)).toEqual([
      CODE.responseInvalid
    ]);
  });

  test('rows are emitted in name order regardless of feed order', () => {
    const { manifest } = parseFossabot(
      feed([command({ name: 'zebra' }), command({ name: 'apple' }), command({ name: 'Mango' })])
    );
    expect(manifest.commands?.map((c) => c.name)).toEqual(['apple', 'mango', 'zebra']);
  });

  test('cooldowns are taken at their widest and clamped when present', () => {
    const { manifest } = parseFossabot(
      feed([
        command({ name: 'a', user_cooldown: 5, global_cooldown: 20 }),
        command({ name: 'b', cooldown: 999_999 })
      ])
    );
    expect(manifest.commands?.[0].cooldown_seconds).toBe(20);
    expect(manifest.commands?.[1].cooldown_seconds).toBe(86400);
  });
});

describe('permissions', () => {
  test('an empty role list is everyone', () => {
    const { manifest, diagnostics } = parseFossabot(feed([command({ role_ids: [] })]));
    expect(manifest.commands?.[0].permission).toBe('everyone');
    expect(diagnostics).toEqual([]);
  });

  test('several roles resolve to the most permissive mapped tier', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ role_ids: ['242753', '242749'] })])
    );
    expect(manifest.commands?.[0].permission).toBe('mod');
    expect(diagnostics).toEqual([]);
  });

  test('an unrecognized role is named and the mapped ones still decide', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ role_ids: ['242755', '242753'] })])
    );
    expect(manifest.commands?.[0].permission).toBe('broadcaster');
    expect(codesOf(diagnostics)).toEqual([CODE.permissionUnmapped]);
    expect(manifest.commands?.[0].warnings?.[0]).toContain('[Imported] Channel Editor');
  });

  test('a role nothing maps lands on everyone with the same warn', () => {
    const { manifest, diagnostics } = parseFossabot(feed([command({ role_ids: ['242755'] })]));
    expect(manifest.commands?.[0].permission).toBe('everyone');
    expect(codesOf(diagnostics)).toEqual([CODE.permissionUnmapped]);
  });

  test('a role id absent from the roles table warns instead of widening silently', () => {
    const { manifest, diagnostics } = parseFossabot(feed([command({ role_ids: ['404404'] })]));
    expect(manifest.commands?.[0].permission).toBe('everyone');
    expect(codesOf(diagnostics)).toEqual([CODE.permissionUnmapped]);
  });
});

describe('references', () => {
  test('a reference is replaced by the referenced response and then translated', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([
        command({ name: 'playlist', response: '$(references music)' }),
        command({ name: 'music', response: 'Spotify for $(user)' })
      ])
    );
    expect(manifest.commands?.[0]).toMatchObject({
      name: 'music',
      responses: ['Spotify for {user}']
    });
    expect(manifest.commands?.[1]).toMatchObject({
      name: 'playlist',
      responses: ['Spotify for {user}']
    });
    expect(diagnostics).toEqual([]);
  });

  test('an alias resolves a reference too, case-insensitively', () => {
    const { manifest } = parseFossabot(
      feed([
        command({ name: 'sensitivity', response: '$(references SENS)' }),
        command({ name: 'settings', aliases: ['sens'], response: 'DPI 1200' })
      ])
    );
    expect(manifest.commands?.find((c) => c.name === 'sensitivity')?.responses).toEqual(['DPI 1200']);
  });

  test('a reference to something not in the feed stays literal and warns', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ response: '$(references nowhere)' })])
    );
    expect(manifest.commands?.[0].responses).toEqual(['$(references nowhere)']);
    expect(codesOf(diagnostics)).toEqual([CODE.variableUnmapped]);
  });

  test('a reference cycle terminates instead of hanging', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([
        command({ name: 'ping', response: 'p $(references pong)' }),
        command({ name: 'pong', response: 'q $(references ping)' })
      ])
    );
    expect(manifest.commands?.map((c) => c.name)).toEqual(['ping', 'pong']);
    expect(codesOf(diagnostics)).toEqual([CODE.variableUnmapped, CODE.variableUnmapped]);
  });
});

describe('urlfetch synthesis', () => {
  test('customapi with a URL becomes a definition with a legal slug', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ name: 'weather', response: 'now: $(customapi https://api.example.com/w)' })])
    );
    expect(manifest.commands?.[0].responses).toEqual(['now: {urlfetch:fossabot_weather}']);
    expect(manifest.fetches).toEqual([
      { name: 'fossabot_weather', url: 'https://api.example.com/w', source: 'fossabot' }
    ]);
    for (const f of manifest.fetches ?? []) expect(isValidFetchDefName(f.name)).toBe(true);
    expect(validateManifest(manifest).filter((d) => d.severity === 'error')).toEqual([]);
    expect(diagnostics).toEqual([]);
  });

  test('two URLs in one command take slot suffixes, the same URL shares one', () => {
    const { manifest } = parseFossabot(
      feed([
        command({
          name: 'x',
          response: '$(customapi https://a.example/1) $(customapi https://a.example/2) $(customapi https://a.example/1)'
        })
      ])
    );
    expect(manifest.fetches?.map((f) => f.name)).toEqual(['fossabot_x', 'fossabot_x_2']);
    expect(manifest.commands?.[0].responses).toEqual([
      '{urlfetch:fossabot_x} {urlfetch:fossabot_x_2} {urlfetch:fossabot_x}'
    ]);
  });

  test('a bare customapi carries no URL, so it stays literal and warns', () => {
    const { manifest, diagnostics } = parseFossabot(
      feed([command({ response: 'Current song: $(customapi)' })])
    );
    expect(manifest.commands?.[0].responses).toEqual(['Current song: $(customapi)']);
    expect(manifest.fetches).toBeUndefined();
    expect(codesOf(diagnostics)).toEqual([CODE.variableUnmapped]);
  });
});

// --- golden ------------------------------------------------------------------

interface Golden {
  manifest: unknown;
  diagnostics: { severity: string; item_index: number; code: string }[];
}

describe('fixture golden', () => {
  const fixture = readFileSync(join(here, 'testdata/fossabot-fixture.json'));
  const golden: Golden = JSON.parse(readFileSync(join(here, 'testdata/fossabot-golden.json'), 'utf8'));
  const parsed = parseFossabot(new Uint8Array(fixture));

  test('manifest matches the committed bytes', () => {
    expect(parsed.manifest).toEqual(golden.manifest);
  });

  test('diagnostic sequence matches the committed bytes', () => {
    expect(
      parsed.diagnostics.map((d) => ({ severity: d.severity, item_index: d.item_index, code: d.code }))
    ).toEqual(golden.diagnostics);
  });

  test('the golden manifest is itself valid for commit', () => {
    expect(validateManifest(parsed.manifest).filter((d) => d.severity === 'error')).toEqual([]);
  });
});

// --- fetch flow (public cached API) ------------------------------------------

// Local stand-in server, mirroring nightbot.test.ts: records every request
// (snapshotted eagerly, Bun recycles Request internals once the handler
// resolves) and answers via `handler`.
async function withTestServer(
  handler: (req: Request) => Response | Promise<Response>,
  run: (baseUrl: string, paths: string[]) => Promise<void>
): Promise<void> {
  const paths: string[] = [];
  const server = Bun.serve({
    port: 0,
    async fetch(req) {
      paths.push(new URL(req.url).pathname);
      return handler(req);
    }
  });
  try {
    await run(`http://127.0.0.1:${server.port}`, paths);
  } finally {
    server.stop(true);
  }
}

const BY_SLUG = '/api/v2/cached/channels/by-slug/myth';
const COMMANDS = '/api/v2/cached/channels/30602/commands';

function liveFeed(req: Request): Response {
  const path = new URL(req.url).pathname;
  if (path === BY_SLUG)
    return Response.json({ channel: { id: '30602', login: 'myth', slug: 'myth' }, parent: {} });
  if (path === COMMANDS) return Response.json({ roles: ROLES, commands: [command()] });
  return new Response('not found', { status: 404 });
}

describe('fetch flow', () => {
  test('resolves the slug then reads the directory, parseable as-is', async () => {
    await withTestServer(liveFeed, async (baseUrl, paths) => {
      const env = await fetchFossabot('  MYTH  ', { baseUrl });
      expect(paths).toEqual([BY_SLUG, COMMANDS]);
      expect(parseFossabot(bytes(env)).manifest.commands).toHaveLength(1);
    });
  });

  test('an unknown channel reads as prose, not as a status code', async () => {
    await withTestServer(
      () => new Response('{"error":"not found"}', { status: 404 }),
      async (baseUrl) => {
        const err = await fetchFossabot('nobody', { baseUrl }).catch((e) => e as Error);
        expect(err).toBeInstanceOf(FossabotFetchError);
        expect(err.message).toBe('fossabot: no Fossabot channel named "nobody"');
      }
    );
  });

  test('a malformed channel name is refused before any transport', async () => {
    await withTestServer(liveFeed, async (baseUrl, paths) => {
      await expect(fetchFossabot('   ', { baseUrl })).rejects.toThrow(/channel name is required/);
      await expect(fetchFossabot('has spaces', { baseUrl })).rejects.toThrow(/is not a channel name/);
      await expect(fetchFossabot('../../etc/passwd', { baseUrl })).rejects.toThrow(/is not a channel name/);
      expect(paths).toEqual([]);
    });
  });

  test('a hanging upstream times out per call', async () => {
    await withTestServer(
      () => new Promise<Response>(() => {}),
      async (baseUrl) => {
        const err = await fetchFossabot('myth', { baseUrl, timeoutMs: 50 }).catch((e) => e as Error);
        expect(err).toBeInstanceOf(FossabotFetchError);
        expect(err.message).toContain('request timed out');
      }
    );
  });

  test('an oversized body is refused instead of being buffered', async () => {
    // 17 MiB in 1 MiB chunks: one past the 16 MiB cap, and finite so the
    // server side cannot spin forever if the cap ever stops firing.
    let sent = 0;
    const flood = new ReadableStream<Uint8Array>({
      pull(controller) {
        if (sent++ >= 17) return controller.close();
        controller.enqueue(new Uint8Array(1 << 20));
      }
    });
    await withTestServer(
      () => new Response(flood, { headers: { 'content-type': 'application/json' } }),
      async (baseUrl) => {
        const err = await fetchFossabot('myth', { baseUrl }).catch((e) => e as Error);
        expect(err).toBeInstanceOf(FossabotFetchError);
        expect(err.message).toContain('body exceeds');
      }
    );
  });
});
