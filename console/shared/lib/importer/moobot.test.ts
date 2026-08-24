// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Golden PARITY test: the browser-side Moobot parser (moobot.ts) was a port of
// app/importer/source/moobot, which was deleted when the importer service
// folded into the dashboard (2026-08-23). This suite replays the fixture
// corpus through the port and asserts the outputs match the committed golden —
// same manifest bytes, same detect verdict, same diagnostic
// {severity,item_index,code} sequence. The corpus passed against the Go
// implementation byte-for-byte before that implementation was removed; the
// fixture itself now lives HERE (testdata/moobot-fixture.json) so the suite is
// self-contained.
//
// The expectations in testdata/moobot-golden.json are COMMITTED and
// regenerated only deliberately:
//
//	IMPORTER_MOOBOT_DUMP_JSON=<repo>/console/shared/lib/importer/testdata/moobot-golden.json \
//	  go test ./app/importer/source/moobot -run TestDumpPortableGolden
//
// (Go command kept for history; today a deliberate regeneration means running
// the parse by hand and reviewing the diff before committing it.)

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { describe, expect, test } from 'bun:test';
import { detectMoobot, MoobotExportError, parseMoobot } from './moobot';
import type { ImportDiagnostic, ImportManifest } from './types';
import { findCollisions } from './validate';

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
// differs but content — all we compare — is identical).
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
    expect(manifest.commands![0].responses![0]).toBe('Temp: {urlfetch:moobot-weather}');
    // Deliberately NO url: Moobot exports carry none; the shell is a
    // placeholder until the broadcaster re-enters it.
    expect(manifest.fetches).toEqual([{ name: 'moobot-weather', source: 'moobot' }]);
  });

  test('json.N tags map to slug-N; plain and slots coexist as distinct defs', () => {
    const { manifest } = parseMoobot(
      exportWith('<urlfetch.plain> | <urlfetch.json.1> | <urlfetch.json.3> | <urlfetch.json.10>')
    );
    expect(manifest.commands![0].responses![0]).toBe(
      '{urlfetch:moobot-weather} | {urlfetch:moobot-weather-1} | {urlfetch:moobot-weather-3} | {urlfetch:moobot-weather-10}'
    );
    expect(manifest.fetches!.map((f) => f.name)).toEqual([
      'moobot-weather',
      'moobot-weather-1',
      'moobot-weather-3',
      'moobot-weather-10'
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
      expect(w.message).toContain('re-enter the URL for');
    }
    expect(warns[0].message).toContain('{urlfetch:moobot-weather}');
    expect(warns[1].message).toContain('"moobot-weather-2"');
  });

  test('command name normalization feeds the slug (!Weather folds onto weather)', () => {
    const { manifest } = parseMoobot(exportWith('<urlfetch.plain>', '!Weather'));
    expect(manifest.fetches!.map((f) => f.name)).toEqual(['moobot-weather']);
  });

  test('re-import idempotence: same export parses to identical bytes', () => {
    const bytes = exportWith('<urlfetch.plain> <urlfetch.json.1>');
    expect(JSON.stringify(parseMoobot(bytes))).toBe(JSON.stringify(parseMoobot(bytes)));
  });

  test('slug collisions with existing channel items surface via CollisionRef', () => {
    const { manifest } = parseMoobot(exportWith('<urlfetch.plain>', 'weather'));
    // The slug itself ("moobot-weather") is the name that could clash with an
    // existing item — a plain "weather" command on the channel does not.
    expect(findCollisions(['moobot-weather'], manifest)).toEqual([{ kind: 'fetch', name: 'moobot-weather' }]);
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
