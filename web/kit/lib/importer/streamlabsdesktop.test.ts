// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Parity suite for streamlabsdesktop.ts, the port of
// app/importer/source/streamlabsdesktop. Two layers:
//
//  1. Golden replay: testdata/slcb-golden.json pins Parse's full manifest +
//     diagnostics for the Go suite's programmatically built Chatbot.db corpus
//     (decoded from its golden.txt during the port). The DBs are rebuilt here
//     with sql.js from the SAME fixtureSpec tables, never committed binaries.
//     A diff means every future SLCB import's translation changed on purpose.
//  2. Unit vectors lifted verbatim from the Go package's tests (parameters,
//     permission table, quote-date layouts, schema fallbacks, detect).

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { beforeAll, describe, expect, test } from 'bun:test';
import initSqlJs from 'sql.js';
import {
  DEFAULT_TIMER_INTERVAL_SECONDS,
  StreamLabsDesktopError,
  detectStreamLabsDesktop,
  fetchStreamLabsDesktop,
  mapPermissionSLCB,
  parseQuoteDate,
  parseStreamLabsDesktop,
  translateVariables
} from './streamlabs-desktop';
import { CODE, validateManifest } from './validate';

const here = dirname(import.meta.path);
const SQL = await initSqlJs();

// --- fixture DB builder (ported from streamlabsdesktop_test.go) ---------------

interface CmdRow {
  name: string;
  response: string;
  perm?: string; // '-' omits the Permission column entirely
  cooldown?: number;
  enabled?: string;
  typ?: string;
}
interface Spec {
  commands?: CmdRow[];
  timers?: string[];
  quotes?: [string, string][]; // [text, date]
  dropTables?: string[]; // names to omit entirely (missing-table paths)
}

const SCHEMA = [
  `CREATE TABLE Commands (
		Id INTEGER PRIMARY KEY,
		Name TEXT,
		Response TEXT,
		Permission TEXT,
		Cooldown INTEGER,
		Enabled TEXT,
		Type TEXT
	)`,
  `CREATE TABLE Timers (
		Id INTEGER PRIMARY KEY,
		Message TEXT
	)`,
  `CREATE TABLE Quotes (
		Id INTEGER PRIMARY KEY,
		Quote TEXT,
		Game TEXT,
		Date TEXT
	)`
];

// openFixtureDB creates the schema minus any dropped tables; one builder per
// table fills it.
function openFixtureDB(spec: Spec) {
  const db = new SQL.Database();
  for (const stmt of SCHEMA) {
    const table = stmt.slice('CREATE TABLE '.length).split(' ')[0];
    if (spec.dropTables?.includes(table)) continue;
    db.run(stmt);
  }
  return db;
}

// buildCommandsTable inserts one row per command, omitting each optional
// column whose value is unset ('-' on perm omits the Permission column
// entirely).
function buildCommandsTable(db: ReturnType<typeof SQL.Database>, commands: CmdRow[]): void {
  for (const c of commands) {
    const cols = ['Name', 'Response'];
    const args: unknown[] = [c.name, c.response];
    if (c.perm && c.perm !== '-') {
      cols.push('Permission');
      args.push(c.perm);
    }
    if (c.cooldown) {
      cols.push('Cooldown');
      args.push(c.cooldown);
    }
    if (c.enabled) {
      cols.push('Enabled');
      args.push(c.enabled);
    }
    if (c.typ) {
      cols.push('Type');
      args.push(c.typ);
    }
    db.run(
      `INSERT INTO Commands (${cols.join(',')}) VALUES (${cols.map(() => '?').join(',')})`,
      args
    );
  }
}

function buildTimersTable(db: ReturnType<typeof SQL.Database>, timers: string[]): void {
  timers.forEach((msg, i) => db.run(`INSERT INTO Timers (Id, Message) VALUES (?, ?)`, [i, msg]));
}

function buildQuotesTable(db: ReturnType<typeof SQL.Database>, quotes: [string, string][]): void {
  quotes.forEach((qt, i) =>
    db.run(`INSERT INTO Quotes (Id, Quote, Game, Date) VALUES (?, ?, ?, ?)`, [i, qt[0], 'Some Game', qt[1]])
  );
}

function buildFixtureDB(spec: Spec): Uint8Array {
  const db = openFixtureDB(spec);
  buildCommandsTable(db, spec.commands ?? []);
  buildTimersTable(db, spec.timers ?? []);
  buildQuotesTable(db, spec.quotes ?? []);
  return db.export();
}

const fullSpec: Spec = {
  commands: [
    { name: '!Lurk', response: '$username is now lurking!', perm: 'Everyone', cooldown: 30 },
    { name: '!hug', response: '/me hugs $targetname $randnum(1,5) times!', perm: '+m' },
    { name: '!cookie', response: '$desc(Cookie counter) $count cookies eaten! $checkcount(!lurk) via $readapi(https://example.api)', perm: 'Regular', cooldown: -5 },
    { name: '!multi', response: 'line one\nline two\r\n\nline three' },
    { name: '!slots', response: '$arg1 vs $arg2 who wins?!', perm: 'Wizard' },
    { name: '!gone', response: 'disabled', perm: 'Subscriber', enabled: 'False' },
    { name: '!viponly', response: 'vip greeting $dummyormsg', perm: 'VIP' },
    { name: '!caster', response: 'only streamer sees', perm: 'Streamer' },
    { name: '!editor', response: 'editor tool', perm: '+e' },
    { name: '!quote', response: '$randquote', perm: 'Subscriber' }
  ],
  timers: ['Follow $mychannel for $randnum(60) minute drops!', '$count uses and counting!'],
  quotes: [
    ['I am a cat! - AnkhHeart', '01/02/2015 3:04 PM'],
    ['Duct tape solves all problems!', '2015-06-07 08:09:10'],
    ['Unparseable date quote', 'not a date at all']
  ]
};

describe('golden replay', () => {
  const golden: { name: string; manifest: Record<string, unknown>; diags: unknown[] }[] = JSON.parse(
    readFileSync(join(here, 'testdata/slcb-golden.json'), 'utf8')
  );

  const specs: [string, Spec][] = [
    ['full_feature_db', fullSpec],
    ['alternate_column_spellings', { commands: [{ name: '!alt', response: 'hey $tousername', perm: 'Mod' }] }],
    [
      'missing_quotes_table_and_clamped_cooldown',
      { dropTables: ['Quotes'], commands: [{ name: '!only', response: 'just me', cooldown: 90000 }] }
    ],
    ['empty_tables', {}]
  ];

  test('corpus covers exactly the committed golden cases', () =>
    expect(specs.map(([n]) => n)).toEqual(golden.map((g) => g.name)));

  for (const [label, spec] of specs) {
    test(`${label}: full manifest + diagnostics byte-exact vs the Go fixture`, async () => {
      const { manifest, diagnostics } = await parseStreamLabsDesktop(buildFixtureDB(spec));
      const want = golden.find((g) => g.name === label)!;
      expect(manifest).toEqual(want.manifest as never);
      expect(diagnostics).toEqual(want.diags as never); // messages included
    });
  }


  test('parse is deterministic', async () => {
    const bytes = buildFixtureDB(fullSpec);
    const a = JSON.stringify(await parseStreamLabsDesktop(bytes));
    const b = JSON.stringify(await parseStreamLabsDesktop(bytes));
    expect(a).toBe(b);
  });
});

// unitSpec mirrors the fixture in Go's TestParse_FullFixture (one command and
// one quote fewer than the golden corpus's full_spec).
const unitSpec: Spec = {
  ...fullSpec,
  commands: fullSpec.commands!.filter((c) => c.name !== '!quote'),
  quotes: [
    ['I am a cat! - AnkhHeart', '01/02/2015 3:04 PM'],
    ['Unparseable date quote', 'not a date at all']
  ]
};

describe('phase 6: $readapi urlfetch synthesis', () => {
  test('a readapi call becomes a definition with a legal slug', async () => {
    const { manifest, diagnostics } = await parseStreamLabsDesktop(
      buildFixtureDB({ commands: [{ name: '!weather', response: 'now: $readapi(https://api.example.com/w)' }] })
    );
    expect(manifest.commands?.[0].responses).toEqual(['now: {urlfetch:slcb_weather}']);
    expect(manifest.fetches).toEqual([
      { name: 'slcb_weather', url: 'https://api.example.com/w', source: 'streamlabs_desktop' }
    ]);
    // phase 6: a successful synthesis now warns, naming the slug and URL.
    expect(diagnostics.map((d) => d.code)).toEqual(['fetch_def_created']);
  });

  test('the same URL twice in one command shares its definition', async () => {
    const { manifest } = await parseStreamLabsDesktop(
      buildFixtureDB({
        commands: [{ name: '!x', response: '$readapi(https://a.example/1) $readapi(https://a.example/1)' }]
      })
    );
    expect(manifest.fetches).toHaveLength(1);
    expect(manifest.commands?.[0].responses).toEqual(['{urlfetch:slcb_x} {urlfetch:slcb_x}']);
  });

  test('a URL built out of another $param is never baked into a definition', async () => {
    const { manifest, diagnostics } = await parseStreamLabsDesktop(
      buildFixtureDB({ commands: [{ name: '!q', response: '$readapi(https://a.example/?u=$username)' }] })
    );
    expect(manifest.fetches).toBeUndefined();
    expect(manifest.commands?.[0].responses).toEqual(['$readapi(https://a.example/?u=$username)']);
    expect(diagnostics.some((d) => d.code === CODE.variableUnmapped)).toBe(true);
  });

  test('a non-https URL is refused rather than synthesized dead', async () => {
    const { manifest, diagnostics } = await parseStreamLabsDesktop(
      buildFixtureDB({ commands: [{ name: '!f', response: '$readapi(ftp://a.example/x)' }] })
    );
    expect(manifest.fetches).toBeUndefined();
    expect(diagnostics.some((d) => d.code === CODE.variableUnmapped)).toBe(true);
  });
});

describe('full-fixture assertions (from TestParse_FullFixture)', () => {
  let parsed: Awaited<ReturnType<typeof parseStreamLabsDesktop>>;
  beforeAll(async () => {
    parsed = await parseStreamLabsDesktop(buildFixtureDB(unitSpec));
  });

  test('commands count after disabled skip', () => expect(parsed.manifest.commands).toHaveLength(8));

  test('lurk/hug/cookie/viponly/caster/editor translations land', () => {
    const byName = new Map(parsed.manifest.commands!.map((c) => [c.name.replace(/^!/, '').toLowerCase(), c]));
    expect(byName.get('lurk')).toMatchObject({
      permission: 'everyone',
      cooldown_seconds: 30,
      responses: ['{user} is now lurking!']
    });
    expect(byName.get('hug')).toMatchObject({
      permission: 'mod',
      responses: ['/me hugs {touser} {random:1-5} times!']
    });
    expect(byName.get('cookie')).toMatchObject({
      permission: 'everyone',
      responses: ['{count} cookies eaten! {counter:lurk} via {urlfetch:slcb_cookie}']
    });
    expect(byName.get('multi')!.responses).toEqual(['line one', 'line two', 'line three']);
    expect(byName.get('slots')).toMatchObject({ permission: 'everyone', responses: ['{1} vs {2} who wins?!'] });
    expect(byName.get('viponly')).toMatchObject({ permission: 'vip', responses: ['vip greeting {args}'] });
    expect(byName.get('caster')!.permission).toBe('broadcaster');
    expect(byName.get('editor')!.permission).toBe('lead_mod');

    // Diagnostic attribution spot-checks (indexes are post-sort positions).
    const idxOf = (name: string): number =>
      parsed.manifest.commands!.findIndex((c) => c.name.toLowerCase().replace(/^!/, '') === name);
    const codesAt = (i: number): Set<string> =>
      new Set(parsed.diagnostics.filter((d) => d.item_index === i).map((d) => d.code));
    expect(codesAt(idxOf('cookie'))).toContain('command_permission_unmapped');
    // phase 6: $readapi(...) now maps onto {urlfetch:slcb_cookie} + a real
    // definition (see the fixture's own fetches assertion below) instead of
    // marking the whole command script-dependent.
    expect(codesAt(idxOf('cookie'))).not.toContain('command_script_dependent');
    // !slots ($arg1 vs $arg2) no longer trips a variable_unmapped warning
    // (phase 6 deleted the all-or-nothing $argN rule this used to hit): both
    // slots map onto their own positional word independently. Its perm
    // ('Wizard') is still unrecognized, so that diagnostic remains.
    expect(codesAt(idxOf('slots'))).toEqual(new Set(['command_permission_unmapped']));
  });

  test('timers: defaulted global interval + randnum/count translation', () => {
    expect(parsed.manifest.timers).toHaveLength(2);
    expect(parsed.manifest.timers![0].message).toBe('$count uses and counting!');
    expect(parsed.manifest.timers![0].interval_seconds).toBe(DEFAULT_TIMER_INTERVAL_SECONDS);
    expect(parsed.manifest.timers![1].message).toBe('Follow {channel} for {random:1-60} minute drops!');
  });

  test('quotes: date layouts parse to UTC RFC 3339 or drop with a warn', () => {
    expect(parsed.manifest.quotes).toHaveLength(2);
    expect(parsed.manifest.quotes![0].created_at).toBe('2015-01-02T15:04:00Z');
    expect(parsed.manifest.quotes![0].added_by).toBeUndefined(); // no author column value in this spec
    expect(parsed.manifest.quotes![1].created_at).toBeUndefined();
    expect(parsed.diagnostics.some((d) => d.code === 'quote_date_unparsed')).toBe(true);

    const notes = parsed.diagnostics.filter((d) => d.code === 'manifest_source_note').map((d) => d.message);
    expect(notes.some((m) => m.includes(DEFAULT_TIMER_INTERVAL_SECONDS.toString()))).toBe(true);
    expect(notes.some((m) => m.includes('disabled'))).toBe(true);
  });

  test('nothing in the fixture carries an error commit would skip', () => {
    for (const d of validateManifest(parsed.manifest)) {
      expect(d.severity).not.toBe('error');
    }
  });
});

describe('schema fallbacks', () => {
  test('singular Timer table parses (missing-table path)', async () => {
    const db = new SQL.Database();
    db.run(`CREATE TABLE Timer (Message TEXT)`);
    const r = await parseStreamLabsDesktop(db.export());
    // The singular spelling satisfies the timer candidates, so no timers
    // section diagnostic fires, exactly like the Go fixture asserts.
    expect(r.manifest.timers).toBeUndefined();
    expect(r.diagnostics.some((d) => d.message.includes('"timers"'))).toBe(false);
    expect(r.diagnostics.some((d) => d.message.includes('"commands"'))).toBe(true);
  });

  test('empty tables produce the nothing-importable note', async () => {
    const r = await parseStreamLabsDesktop(buildFixtureDB({}));
    expect(r.diagnostics.some((d) => d.message.includes('no importable'))).toBe(true);
  });

  test('not a SQLite database rejects as parse_failed material', async () => {
    for (const raw of [new TextEncoder().encode('this is definitely not a database'), new Uint8Array(0)]) {
      await expect(parseStreamLabsDesktop(raw)).rejects.toBeInstanceOf(StreamLabsDesktopError);
    }
    const junkMagic = new TextEncoder().encode('SQLite format 3\x00' + 'junkjunk'.repeat(64));
    await expect(parseStreamLabsDesktop(junkMagic)).rejects.toBeInstanceOf(StreamLabsDesktopError);
  });
});

describe('detect', () => {
  test('chatbot.db fixture detects', async () => {
    expect(await detectStreamLabsDesktop(buildFixtureDB({ commands: [{ name: '!hi', response: 'hello' }] }))).toBe(
      true
    );
  });
  test('foreign inputs never claim detection', async () => {
    expect(await detectStreamLabsDesktop(new Uint8Array(0))).toBe(false);
    expect(await detectStreamLabsDesktop(new TextEncoder().encode('{"commands":[]}'))).toBe(false);
    expect(await detectStreamLabsDesktop(new TextEncoder().encode('<?xml version="1.0"?><config/>'))).toBe(false);
    const magicOnly = new TextEncoder().encode('SQLite format 3\x00');
    expect(await detectStreamLabsDesktop(magicOnly)).toBe(false);
  });
  test('valid sqlite without feature tables is not claimed', async () => {
    const db = new SQL.Database();
    db.run(`CREATE TABLE UsersView (Id INTEGER PRIMARY KEY)`);
    expect(await detectStreamLabsDesktop(db.export())).toBe(false);
  });
});

test('fetch passthrough', () => {
  expect(() => fetchStreamLabsDesktop(new Uint8Array(0))).toThrow(/upload your Chatbot\.db file/);
  const bytes = new TextEncoder().encode('bytes');
  expect(fetchStreamLabsDesktop(bytes)).toBe(bytes);
});

describe('$parameter translation (vectors from parameters_test)', () => {
  interface Case {
    name: string;
    input: string;
    cmd?: string;
    want: string;
    ext?: boolean;
    warnSub?: string[];
    noWarn?: boolean;
  }
  const cases: Case[] = [
    { name: 'plain', input: 'hello world', cmd: 'x', want: 'hello world', noWarn: true },
    { name: 'username', input: '$username hi', cmd: 'x', want: '{user} hi', noWarn: true },
    { name: 'userid maps to user.id', input: 'gg $userid', cmd: 'x', want: 'gg {user.id}', noWarn: true },
    {
      name: 'target variants',
      input: '$targetname/$tousername/$touser/$target',
      cmd: 'x',
      want: '{touser}/{touser}/{touser}/{touser}',
      noWarn: true
    },
    { name: 'channel', input: 'follow $mychannel', cmd: 'x', want: 'follow {channel}', noWarn: true },
    { name: 'msg', input: 'you said $msg', cmd: 'x', want: 'you said {args}', noWarn: true },
    { name: 'dummyormsg', input: 'poke $dummyormsg', cmd: 'x', want: 'poke {args}', noWarn: true },
    { name: 'randnum two args', input: '$randnum(1,7)', cmd: 'x', want: '{random:1-7}', noWarn: true },
    { name: 'randnum reversed', input: '$randnum(9,2)', cmd: 'x', want: '{random:2-9}', noWarn: true },
    { name: 'randnum single', input: '$randnum(60)', cmd: 'x', want: '{random:1-60}', noWarn: true },
    {
      name: 'randnum garbage stays',
      input: 'roll $randnum(abc)',
      cmd: 'x',
      want: 'roll $randnum(abc)',
      warnSub: ['$randnum']
    },
    // $count auto-increments per run upstream, same as {count}/{uses} here, so
    // it maps onto that rather than a named {counter:<cmdName>} (which would
    // silently create/bind a channel counter the broadcaster never named).
    // {count} takes no payload, so the command name plays no part any more.
    // Mapped, but its MEANING changed (a live running total -> this bot's own
    // use count from zero), so it warns once even though the span itself
    // translated cleanly — see SLCB_CODE.countRemapped.
    { name: 'count in a command', input: '$count times', cmd: 'Death', want: '{count} times', warnSub: ['{count}'] },
    { name: 'count ignores the command name entirely', input: '!c $count', cmd: '!Cookie', want: '!c {count}', warnSub: ['{count}'] },
    { name: 'count in timer stays', input: 'timer $count', cmd: '', want: 'timer $count', warnSub: ['$count'] },
    {
      name: 'checkcount normalizes',
      input: '$checkcount(!Hug Me) hugs',
      cmd: 'x',
      want: '{counter:hug me} hugs',
      noWarn: true
    },
    {
      // phase 6: $readapi now maps onto {urlfetch:<slug>} (see the golden
      // replay fixture and the dedicated readapi tests below) when a sink is
      // available; this vector table calls translateVariables directly with
      // no sink (only extract.ts wires one, per command), so both remaining
      // cases here exercise the no-sink degrade path instead of "external".
      name: 'readapi with no sink stays literal and warns',
      input: 'temp: $readapi(http://x/y?a=b)',
      cmd: 'x',
      want: 'temp: $readapi(http://x/y?a=b)',
      warnSub: ['$readapi']
    },
    {
      name: 'nested parens, no sink stays literal and warns',
      input: '$readapi(https://x/a(b)) end',
      cmd: 'x',
      want: '$readapi(https://x/a(b)) end',
      warnSub: ['$readapi']
    },
    {
      name: 'savetofile external',
      input: '$savetofile("f.txt","v","ok","no")',
      cmd: 'x',
      want: '$savetofile("f.txt","v","ok","no")',
      ext: true,
      noWarn: true
    },
    {
      name: 'countdown date-only maps',
      input: '$countdown(2026-12-25)',
      cmd: 'x',
      want: '{countdown:2026-12-25}',
      noWarn: true
    },
    {
      name: 'countup free-form date normalizes to RFC3339',
      input: '$countup(Jan 1 2026 00:00:00 UTC)',
      cmd: 'x',
      want: '{countup:2026-01-01T00:00:00.000Z}',
      noWarn: true
    },
    {
      name: 'countdown unparsable date warns',
      input: '$countdown(whenever)',
      cmd: 'x',
      want: '$countdown(whenever)',
      warnSub: ['date this bot could not read']
    },
    { name: 'points maps', input: '$points points!', cmd: 'x', want: '{points} points!', noWarn: true },
    { name: 'currencyname maps', input: 'earn $currencyname now', cmd: 'x', want: 'earn {points.name} now', noWarn: true },
    { name: 'randusername maps', input: 'hi $randusername', cmd: 'x', want: 'hi {random.viewer}', noWarn: true },
    {
      name: 'unknown token warned',
      input: '$givepoints points!',
      cmd: 'x',
      want: '$givepoints points!',
      warnSub: ['$givepoints']
    },
    { name: 'currency dollar untouched', input: 'costs $5 and 50$', cmd: 'x', want: 'costs $5 and 50$', noWarn: true },
    {
      name: 'desc first line stripped',
      input: '$desc(My description)\nreal line',
      cmd: 'x',
      want: 'real line',
      noWarn: true
    },
    {
      name: 'desc mid-text kept literal',
      input: 'start $desc(x) end',
      cmd: 'x',
      want: 'start $desc(x) end',
      noWarn: true
    },
    { name: 'arg1 becomes positional word 1', input: 'slaps $arg1', cmd: 'x', want: 'slaps {1}', noWarn: true },
    // phase 6: $numN's own numeric-only check has no equivalent in this
    // bot's plain positional word, so it warns even though it maps cleanly.
    {
      name: 'num1 becomes positional word 1, warns about the lost numeric check',
      input: 'bet $num1',
      cmd: 'x',
      want: 'bet {1}',
      warnSub: ['only filled this in when the word was a number']
    },
    {
      name: 'arg10 is positional word 10, not arg1',
      input: '$arg10 wins',
      cmd: 'x',
      want: '{10} wins',
      noWarn: true
    },
    {
      name: 'two slots map independently (no more all-or-nothing)',
      input: '$arg1 vs $arg2',
      cmd: 'x',
      want: '{1} vs {2}',
      noWarn: true
    },
    {
      name: 'argl bare means the whole rest, lower-cased',
      input: 'say $argl',
      cmd: 'x',
      want: 'say {args}',
      noWarn: true
    },
    {
      name: 'arglN maps to the rest-from-N slice, warned for lost lower-casing',
      input: 'say $argl2',
      cmd: 'x',
      want: 'say {2:}',
      warnSub: ['lower-cased']
    },
    {
      name: 'arg slot past the positional cap stays literal',
      input: '$arg31',
      cmd: 'x',
      want: '$arg31',
      warnSub: ['30-word limit']
    },
    {
      name: 'mixed known unknown',
      input: '$username gave $givepoints(...) stuff',
      cmd: 'x',
      want: '{user} gave $givepoints(...) stuff',
      warnSub: ['$givepoints']
    },
    {
      name: 'unterminated parens tolerated',
      input: '$randnum(1,7 oops',
      cmd: 'x',
      want: '$randnum(1,7 oops',
      warnSub: ['$randnum']
    },
    { name: 'dummy alone unmapped', input: '$dummy', cmd: 'x', want: '$dummy', warnSub: ['$dummy'] }
  ];

  for (const c of cases) {
    test(c.name, () => {
      const res = translateVariables(c.input, c.cmd ?? '');
      expect(res.text).toBe(c.want);
      expect(res.external).toBe(!!c.ext);
      if (c.noWarn) expect(res.diags).toEqual([]);
      for (const sub of c.warnSub ?? []) {
        expect(res.diags.some((d) => d.message.includes(sub))).toBe(true);
      }
    });
  }
});

describe('SLCB permission table', () => {
  const cases: Record<string, { perm: string; code: string }> = {
    '': { perm: 'everyone', code: '' },
    Everyone: { perm: 'everyone', code: '' },
    '+a': { perm: 'everyone', code: '' },
    Subscriber: { perm: 'sub', code: '' },
    '+s': { perm: 'sub', code: '' },
    VIP: { perm: 'vip', code: '' },
    Moderator: { perm: 'mod', code: '' },
    '+m': { perm: 'mod', code: '' },
    Streamer: { perm: 'broadcaster', code: '' },
    Broadcaster: { perm: 'broadcaster', code: '' },
    Caster: { perm: 'broadcaster', code: '' },
    Editor: { perm: 'lead_mod', code: 'command_permission_adjusted' },
    '+e': { perm: 'lead_mod', code: 'command_permission_adjusted' },
    Regular: { perm: 'everyone', code: 'command_permission_unmapped' },
    '+r': { perm: 'everyone', code: 'command_permission_unmapped' },
    '+gw': { perm: 'sub', code: 'command_permission_adjusted' },
    Invisible: { perm: 'everyone', code: 'command_permission_unmapped' },
    '+p100': { perm: 'everyone', code: 'command_permission_unmapped' },
    wizard: { perm: 'everyone', code: 'command_permission_unmapped' }
  };
  for (const [raw, w] of Object.entries(cases)) {
    test(`${JSON.stringify(raw)} -> ${w.perm}${w.code ? ` + ${w.code}` : ''}`, () => {
      const { perm, diags } = mapPermissionSLCB(raw);
      expect(perm).toBe(w.perm);
      if (w.code === '') expect(diags).toEqual([]);
      else {
        expect(diags).toHaveLength(1);
        expect(diags[0].code).toBe(w.code);
      }
    });
  }
});

describe('quote date layouts (from TestParseQuoteDateLayouts)', () => {
  const cases: Record<string, string | null> = {
    '2015-01-02T15:04:05Z': '2015-01-02T15:04:05Z',
    '2015-01-02 15:04:05': '2015-01-02T15:04:05Z',
    '01/02/2015 3:04 PM': '2015-01-02T15:04:00Z',
    '01/02/2015': '2015-01-02T00:00:00Z',
    '31/12/2015': null // day-first is NOT an SLCB layout; must not guess
  };
  for (const [input, want] of Object.entries(cases)) {
    test(`${input}`, () => expect(parseQuoteDate(input)).toBe(want));
  }
});
