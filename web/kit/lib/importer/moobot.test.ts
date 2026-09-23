// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Golden PARITY test: the browser-side Moobot parser (moobot.ts) was a port of
// app/importer/source/moobot, which was deleted when the importer service
// folded into the dashboard (2026-08-23). This suite replays the fixture
// corpus through the port and asserts the outputs match the committed golden:
// same manifest bytes, same detect verdict, same diagnostic
// {severity,item_index,code} sequence. The corpus passed against the Go
// implementation byte-for-byte before that implementation was removed; the
// fixture itself now lives HERE (testdata/moobot-fixture.json) so the suite is
// self-contained.
//
// The expectations in testdata/moobot-golden.json are COMMITTED and
// regenerated only deliberately:
//
//	IMPORTER_MOOBOT_DUMP_JSON=<repo>/web/kit/lib/importer/testdata/moobot-golden.json \
//	  go test ./app/importer/source/moobot -run TestDumpPortableGolden
//
// (Go command kept for history; today a deliberate regeneration means running
// the parse by hand and reviewing the diff before committing it.)

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import { detectMoobot, MoobotExportError, parseMoobot } from './moobot';
import { translateTags } from './moobot/tags';
import type { TagContext } from './moobot/tags';
import type { ImportDiagnostic, ImportManifest } from './types';
import { findCollisions, isValidFetchDefName } from './validate';

const here = dirname(import.meta.path);

const FIXTURE_PATH = join(here, 'testdata/moobot-fixture.json');
const GOLDEN_PATH = join(here, 'testdata/moobot-golden.json');

interface PortableCase {
  name: string;
  detect: boolean;
  err?: string;
  manifest?: ImportManifest;
  diags?: Pick<ImportDiagnostic, 'severity' | 'item_index' | 'code'>[];
}

const golden: PortableCase[] = JSON.parse(readFileSync(GOLDEN_PATH, 'utf8'));
const fixtureBytes = new Uint8Array(readFileSync(FIXTURE_PATH));

// Corpus mirrors goldenCorpus() in app/importer/source/moobot/golden_test.go,
// rebuilt semantically (Go mutates via map[string]any + re-marshal; key order
// differs but content, all we compare, is identical).
function corpus(): Map<string, Uint8Array> {
  const raw = readFileSync(FIXTURE_PATH, 'utf8');
  const doc = JSON.parse(raw) as Record<string, unknown>;

  // json.Compact equivalent: re-stringify without indentation.
  const compact = JSON.stringify(doc);
  const clone = () => structuredClone(doc) as Record<string, unknown>;
  const settings = (): Record<string, unknown>[] => clone().settings as Record<string, unknown>[];
  const dropSection = (type: string) => {
    const d = clone();
    d.settings = (d.settings as Record<string, unknown>[]).filter((s) => s.type !== type);
    return d;
  };

  const enc = new TextEncoder();
  const b = (v: string | Record<string, unknown>) => enc.encode(typeof v === 'string' ? v : JSON.stringify(v));
  const bomPrefix = (bytes: Uint8Array) => {
    const out = new Uint8Array(bytes.length + 3);
    out.set([0xef, 0xbb, 0xbf]);
    out.set(bytes, 3);
    return out;
  };

  return new Map(
    Object.entries({
      fixture: fixtureBytes,
      compact: b(compact),
      bom: bomPrefix(fixtureBytes),
      empty: new Uint8Array(0),
      'not-json': b('<html>404</html>'),
      'truncated-half': fixtureBytes.slice(0, Math.floor(fixtureBytes.length / 2)),
      'version-2': (() => {
        const d = clone();
        d.version = 2;
        return b(d);
      })(),
      'type-profile': (() => {
        const d = clone();
        d.type = 'profile';
        return b(d);
      })(),
      'no-settings': (() => {
        const d = clone();
        delete d.settings;
        return b(d);
      })(),
      'no-commands': b(dropSection('commands_custom')),
      'no-aliases': b(dropSection('command_aliases')),
      'no-timers': b(dropSection('command_timers')),
      'only-perm-groups': b(`{"version":1,"type":"settings","settings":[{"type":"permission_groups","data":[{"name":"crew"}]}]}`),
      'corrupt-section': b(`{"version":1,"type":"settings","settings":[{"type":"commands_custom","data":true}]}`),
      'minimal-command': b(
        `{"version":1,"type":"settings","settings":[{"type":"commands_custom","data":[{"identifier":"!Hey","text":"yo <username> <random.number> <counter>"}]}]}`
      ),
      'unknown-usergroup': b(
        `{"version":1,"type":"settings","settings":[{"type":"commands_custom","data":[{"identifier":"x","text":"t","trigger_usergroups":[7]}]}]}`
      )
    })
  );
}

// Cases whose hard-failure text wraps the platform JSON library's message
// (encoding/json vs V8), which no portable assertion can pin; presence of a
// failure is still asserted. Every other err is OUR format string and must
// match byte-for-byte.
const ERR_PRESENCE_ONLY = new Set(['not-json', 'truncated-half', 'empty']);

test('parity corpus covers exactly the committed golden cases', () => {
  expect([...corpus().keys()].sort()).toEqual(golden.map((c) => c.name).sort());
});

for (const want of golden) {
  test(`parity: ${want.name}`, () => {
    const bytes = corpus().get(want.name)!;

    let detected = false;
    let threw: Error | null = null;
    let manifest: ImportManifest | undefined;
    let diagnostics: ImportDiagnostic[] = [];
    try {
      detected = detectMoobot(bytes);
      ({ manifest, diagnostics } = parseMoobot(bytes));
    } catch (err) {
      threw = err as Error;
      if (!(threw instanceof MoobotExportError)) throw threw;
      detected = detectMoobot(bytes);
    }

    expect(detected).toBe(want.detect);

    if (want.err !== undefined) {
      expect(threw).not.toBeNull();
      if (!ERR_PRESENCE_ONLY.has(want.name)) expect(threw!.message).toBe(want.err);
    } else {
      expect(threw).toBeNull();
      expect(manifest).toEqual(want.manifest ?? {});
      // Diagnostics compare as ordered {severity,item_index,message-less code}
      // triples: prose embeds Go %q/%v formatting by design.
      expect(
        diagnostics.map(({ severity, item_index, code }) => ({ severity, item_index, code }))
      ).toEqual((want.diags ?? []).map(({ severity, item_index, code }) => ({ severity, item_index, code })));
    }
  });
}

// --- urlfetch mapping (docs/urlfetch/IMPLEMENTATION.md, Phase 4) --------------

function exportWith(text: string, identifier = 'weather'): Uint8Array {
  return new TextEncoder().encode(
    JSON.stringify({
      version: 1,
      type: 'settings',
      settings: [{ type: 'commands_custom', data: [{ identifier, text }] }]
    })
  );
}

describe('urlfetch mapping', () => {
  test('plain tag maps to the bare slug backed by a URL-less shell def', () => {
    const { manifest } = parseMoobot(exportWith('Temp: <urlfetch.plain>'));
    expect(manifest.commands![0].responses![0]).toBe('Temp: {urlfetch:moobot_weather}');
    // Deliberately NO url: Moobot exports carry none; the shell is a
    // placeholder until the broadcaster re-enters it.
    expect(manifest.fetches).toEqual([{ name: 'moobot_weather', source: 'moobot' }]);
  });

  // Regression guard: the slug used to be `moobot-<command>`, which the
  // commands service refuses (^[a-z0-9_]{1,32}$), so every synthesized shell
  // failed at commit with its tokens already in the response text.
  test('every synthesized definition name is one the commands service accepts', () => {
    const { manifest } = parseMoobot(
      exportWith('a <urlfetch.plain> b <urlfetch.json.3>', 'my cool-cmd')
    );
    expect(manifest.fetches!.length).toBe(2);
    for (const f of manifest.fetches!) expect(isValidFetchDefName(f.name)).toBe(true);
  });

  test('json.N tags map to slug-N; plain and slots coexist as distinct defs', () => {
    const { manifest } = parseMoobot(
      exportWith('<urlfetch.plain> | <urlfetch.json.1> | <urlfetch.json.3> | <urlfetch.json.10>')
    );
    expect(manifest.commands![0].responses![0]).toBe(
      '{urlfetch:moobot_weather} | {urlfetch:moobot_weather_1} | {urlfetch:moobot_weather_3} | {urlfetch:moobot_weather_10}'
    );
    expect(manifest.fetches!.map((f) => f.name)).toEqual([
      'moobot_weather',
      'moobot_weather_1',
      'moobot_weather_3',
      'moobot_weather_10'
    ]);
    for (const f of manifest.fetches!) {
      expect(f.url).toBeUndefined();
      expect(f.source).toBe('moobot');
    }
  });

  test('one targeted warn per distinct tag, pointing at the slug to complete', () => {
    const { diagnostics } = parseMoobot(exportWith('<urlfetch.plain> <urlfetch.plain> <urlfetch.json.2>'));
    const warns = diagnostics.filter((d) => d.code === 'command_fetch_url_absent');
    expect(warns).toHaveLength(2);
    for (const w of warns) {
      expect(w.severity).toBe('warn');
      expect(w.item_index).toBe(0);
      expect(w.message).toContain('Re-enter the URL for');
    }
    expect(warns[0].message).toContain('{urlfetch:moobot_weather}');
    expect(warns[1].message).toContain('"moobot_weather_2"');
  });

  test('command name normalization feeds the slug (!Weather folds onto weather)', () => {
    const { manifest } = parseMoobot(exportWith('<urlfetch.plain>', '!Weather'));
    expect(manifest.fetches!.map((f) => f.name)).toEqual(['moobot_weather']);
  });

  test('re-import idempotence: same export parses to identical bytes', () => {
    const bytes = exportWith('<urlfetch.plain> <urlfetch.json.1>');
    expect(JSON.stringify(parseMoobot(bytes))).toBe(JSON.stringify(parseMoobot(bytes)));
  });

  test('slug collisions with existing channel items surface via CollisionRef', () => {
    const { manifest } = parseMoobot(exportWith('<urlfetch.plain>', 'weather'));
    // The slug itself ("moobot_weather") is the name that could clash with an
    // existing item, a plain "weather" command on the channel does not.
    expect(findCollisions(['moobot_weather'], manifest)).toEqual([{ kind: 'fetch', name: 'moobot_weather' }]);
    // The imported command "weather" would of course collide with itself on
    // the channel; only an unrelated existing list stays fully clean.
    expect(findCollisions(['nothing-here'], manifest)).toEqual([]);
  });

  test(`synthesis stops at the commands cap (${2000}); excess tags degrade to literal+warn`, () => {
    const data = Array.from({ length: 2001 }, (_, i) => ({ identifier: `c${i}`, text: '<urlfetch.plain>' }));
    const bytes = new TextEncoder().encode(
      JSON.stringify({ version: 1, type: 'settings', settings: [{ type: 'commands_custom', data }] })
    );
    const { manifest, diagnostics } = parseMoobot(bytes);
    expect(manifest.fetches).toHaveLength(2000);
    const overflow = diagnostics.filter((d) => d.code === 'command_variable_unmapped' && d.item_index === 2000);
    expect(overflow).toHaveLength(1);
    expect(manifest.commands![2000].responses![0]).toContain('<urlfetch.plain>');
  });
});

describe('<counter> remapping onto {count}', () => {
  function exportWithCounter(text: string, counter?: number): Uint8Array {
    const cmd: Record<string, unknown> = { identifier: 'deaths', text };
    if (counter !== undefined) cmd.counter = counter;
    return new TextEncoder().encode(
      JSON.stringify({
        version: 1,
        type: 'settings',
        settings: [{ type: 'commands_custom', data: [cmd] }]
      })
    );
  }

  test('<counter> maps onto bare {count}', () => {
    const { manifest } = parseMoobot(exportWithCounter('Death count: <counter>'));
    expect(manifest.commands![0].responses![0]).toBe('Death count: {count}');
  });

  test('warns once, naming the old value, when the export carries one', () => {
    const { diagnostics } = parseMoobot(exportWithCounter('<counter> deaths', 87));
    const warns = diagnostics.filter((d) => d.code === 'command_count_remapped');
    expect(warns).toHaveLength(1);
    expect(warns[0].severity).toBe('warn');
    expect(warns[0].item_index).toBe(0);
    expect(warns[0].message).toContain('87');
    expect(warns[0].message).toContain('{count}');
  });

  test('warns without an old-value clause when the export carries none', () => {
    const { diagnostics } = parseMoobot(exportWithCounter('<counter> deaths'));
    const warns = diagnostics.filter((d) => d.code === 'command_count_remapped');
    expect(warns).toHaveLength(1);
    expect(warns[0].message).not.toMatch(/\d/);
  });

  test('a response naming <counter> twice still warns once', () => {
    const { diagnostics } = parseMoobot(exportWithCounter('<counter> then <counter> again'));
    expect(diagnostics.filter((d) => d.code === 'command_count_remapped')).toHaveLength(1);
  });

  test('a response with no <counter> tag never warns', () => {
    const { diagnostics } = parseMoobot(exportWithCounter('no counters here', 12));
    expect(diagnostics.filter((d) => d.code === 'command_count_remapped')).toHaveLength(0);
  });
});

// --- phase 6 tag vector table -------------------------------------------------
// One row per Moobot tag this file's TAG_RENDERERS table maps or explicitly
// warns on, run directly through translateTags rather than a full export
// fixture: each row pins exactly what the phase 6 spec named, independent of
// the golden corpus's own coverage (which happens to exercise some of these,
// but not all, and not the warn-vs-map distinction on its own).
describe('phase 6 tag vector table', () => {
  const ctx = (): TagContext => ({ name: 'x', randomTexts: [], fetchDefs: new Map() });

  interface Case {
    name: string;
    input: string;
    text: string;
    unmapped?: string[];
    unrecognized?: string[];
    positionalFallbackLost?: string[];
  }

  const cases: Case[] = [
    { name: 'uptime', input: 'live for <uptime>', text: 'live for {uptime}' },
    { name: 'twitch.title', input: '<twitch.title>', text: '{title}' },
    { name: 'twitch.game', input: '<twitch.game>', text: '{game}' },
    { name: 'twitch.viewers', input: '<twitch.viewers>', text: '{channel.viewers}' },
    { name: 'twitch.followed', input: '<twitch.followed>', text: '{followage}' },
    { name: 'twitch.followers', input: '<twitch.followers>', text: '{followers}' },
    { name: 'twitch.subs.count', input: '<twitch.subs.count>', text: '{subs}' },
    { name: 'random.userlist', input: '<random.userlist> says hi', text: '{random.viewer} says hi' },
    { name: 'time', input: 'it is <time>', text: 'it is {time}' },
    { name: 'args.url', input: '?q=<args.url>', text: '?q={querystring}' },
    {
      name: '<1> maps onto {touser}, the closest fallback-carrying token',
      input: 'hi <1>',
      text: 'hi {touser}'
    },
    {
      name: '<2>..<5> map onto plain positional words and warn about the lost username fallback',
      input: '<2> <3> <4> <5>',
      text: '{2} {3} {4} {5}',
      positionalFallbackLost: ['2', '3', '4', '5']
    },
    {
      name: 'a catalog tag with no mapping warns as unmapped (known)',
      input: '<countdown>',
      text: '<countdown>',
      unmapped: ['countdown']
    },
    {
      name: 'a non-catalog bracketed word warns as unrecognized (phase 6: used to be silent)',
      input: 'feeling <sad> today',
      text: 'feeling <sad> today',
      unrecognized: ['sad']
    }
  ];

  for (const c of cases) {
    test(c.name, () => {
      const res = translateTags(c.input, ctx());
      expect(res.text).toBe(c.text);
      expect(res.unmapped).toEqual(c.unmapped ?? []);
      expect(res.unrecognized).toEqual(c.unrecognized ?? []);
      expect(res.positionalFallbackLost).toEqual(c.positionalFallbackLost ?? []);
    });
  }
});
