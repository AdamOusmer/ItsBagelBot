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
} from './streamlabsdesktop';
import { validateManifest } from './validate';

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

function buildFixtureDB(spec: Spec): Uint8Array {
  const db = new SQL.Database();
  for (const stmt of SCHEMA) {
    const table = stmt.slice('CREATE TABLE '.length).split(' ')[0];
    if (spec.dropTables?.includes(table)) continue;
    db.run(stmt);
  }
  for (const c of spec.commands ?? []) {
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
  (spec.timers ?? []).forEach((msg, i) => db.run(`INSERT INTO Timers (Id, Message) VALUES (?, ?)`, [i, msg]));
  (spec.quotes ?? []).forEach((qt, i) =>
    db.run(`INSERT INTO Quotes (Id, Quote, Game, Date) VALUES (?, ?, ?, ?)`, [i, qt[0], 'Some Game', qt[1]])
  );
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
    ['I am a cat! – AnkhHeart', '01/02/2015 3:04 PM'],
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
    ['I am a cat! – AnkhHeart', '01/02/2015 3:04 PM'],
    ['Unparseable date quote', 'not a date at all']
  ]
};

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
      responses: ['/me hugs {target} {random:1-5} times!']
    });
    expect(byName.get('cookie')).toMatchObject({
      permission: 'everyone',
      responses: ['{counter:cookie} cookies eaten! {counter:lurk} via $readapi(https://example.api)']
    });
    expect(byName.get('multi')!.responses).toEqual(['line one', 'line two', 'line three']);
    expect(byName.get('slots')).toMatchObject({ permission: 'everyone', responses: ['$arg1 vs $arg2 who wins?!'] });
    expect(byName.get('viponly')).toMatchObject({ permission: 'vip', responses: ['vip greeting {args}'] });
    expect(byName.get('caster')!.permission).toBe('broadcaster');
    expect(byName.get('editor')!.permission).toBe('lead_mod');

    // Diagnostic attribution spot-checks (indexes are post-sort positions).
    const idxOf = (name: string): number =>
      parsed.manifest.commands!.findIndex((c) => c.name.toLowerCase().replace(/^!/, '') === name);
    const codesAt = (i: number): Set<string> =>
      new Set(parsed.diagnostics.filter((d) => d.item_index === i).map((d) => d.code));
    expect(codesAt(idxOf('cookie'))).toContain('command_permission_unmapped');
    expect(codesAt(idxOf('cookie'))).toContain('command_script_dependent');
    expect(codesAt(idxOf('slots'))).toContain('command_variable_unmapped');
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
    // section diagnostic fires — exactly like the Go fixture asserts.
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
    { name: 'userid maps to user', input: 'gg $userid', cmd: 'x', want: 'gg {user}', noWarn: true },
    {
      name: 'target variants',
      input: '$targetname/$tousername/$touser/$target',
      cmd: 'x',
      want: '{target}/{target}/{target}/{target}',
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
    { name: 'count uses cmd name', input: '$count times', cmd: 'Death', want: '{counter:death} times', noWarn: true },
    { name: 'count strips bang', input: '!c $count', cmd: '!Cookie', want: '!c {counter:cookie}', noWarn: true },
    { name: 'count in timer stays', input: 'timer $count', cmd: '', want: 'timer $count', warnSub: ['$count'] },
    {
      name: 'checkcount normalizes',
      input: '$checkcount(!Hug Me) hugs',
      cmd: 'x',
      want: '{counter:hug me} hugs',
      noWarn: true
    },
    {
      name: 'readapi literal + external',
      input: 'temp: $readapi(http://x/y?a=b)',
      cmd: 'x',
      want: 'temp: $readapi(http://x/y?a=b)',
      ext: true,
      noWarn: true
    },
    {
      name: 'nested parens external',
      input: '$readapi(https://x/a(b)) end',
      cmd: 'x',
      want: '$readapi(https://x/a(b)) end',
      ext: true,
      noWarn: true
    },
    {
      name: 'savetofile external',
      input: '$savetofile("f.txt","v","ok","no")',
      cmd: 'x',
      want: '$savetofile("f.txt","v","ok","no")',
      ext: true,
      noWarn: true
    },
    { name: 'unknown token warned', input: '$points points!', cmd: 'x', want: '$points points!', warnSub: ['$points'] },
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
    { name: 'single arg1 becomes args', input: 'slaps $arg1', cmd: 'x', want: 'slaps {args}', noWarn: true },
    { name: 'num1 becomes args', input: 'bet $num1', cmd: 'x', want: 'bet {args}', noWarn: true },
    {
      name: 'arg10 not arg1',
      input: '$arg10 wins',
      cmd: 'x',
      want: '$arg10 wins',
      warnSub: ['numbered argument']
    },
    {
      name: 'two slots stay literal',
      input: '$arg1 vs $arg2',
      cmd: 'x',
      want: '$arg1 vs $arg2',
      warnSub: ['numbered argument']
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
