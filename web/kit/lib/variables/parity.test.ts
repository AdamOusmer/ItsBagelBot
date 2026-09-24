// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync } from 'node:fs';
import { join, sep } from 'node:path';
import { tokenHead } from './head';
import { lex } from '../engine/tmpl';
import { TARGETS } from '../importer/targets';
import { VARIABLES } from './variables';
import { forSurface } from './surfaces';
import type { VariableDef } from './types';
import { MODULE_CATALOG } from '../catalog';
import { BUILTIN_COMMANDS } from '../catalog/builtin-commands';
import type { ReplyToken } from '../catalog/module-def';
import en from '../i18n/locales/en.json';
import fr from '../i18n/locales/fr.json';

const GOLDEN_PATH = join(import.meta.dir, '../../../../app/twitch/sesame/engine/scope/testdata/token_catalog.golden.json');

interface GoldenFile {
  note: string;
  families: { id: string; examples: string[]; aliases?: string[] }[];
  surfaces: { timer: string[] };
}

const golden: GoldenFile = JSON.parse(readFileSync(GOLDEN_PATH, 'utf8'));
const goldenExamples = golden.families.flatMap((family) => family.examples);
const goldenHeads = new Set(goldenExamples.map((example) => tokenHead(example)));

const goldenAliasHeads = new Set(golden.families.flatMap((family) => family.aliases ?? []).map(tokenHead));

const REPLY_TOKENS_GOLDEN_PATH = join(import.meta.dir, '../../../../app/twitch/sesame/modules/testdata/reply_tokens.golden.json');

interface ReplyTokensGoldenFile {
  note: string;
  replies: Record<string, string[]>;
}

const replyTokensGolden: ReplyTokensGoldenFile = JSON.parse(readFileSync(REPLY_TOKENS_GOLDEN_PATH, 'utf8'));

const NOT_RESOLVER_OWNED = new Set(['positional', 'if']);

const isDigits = (head: string): boolean => head !== '' && /^\d+$/.test(head);

const isPositionalLeadingSlice = (token: string): boolean => /^\{:\d+\}$/.test(token.trim());

const namesOf = (v: VariableDef): string[] => [v.head, ...(v.aliases ?? [])];

const headSet = new Set(VARIABLES.flatMap(namesOf));

const isCovered = (token: string): boolean => {
  const head = tokenHead(token);
  return isDigits(head) || isPositionalLeadingSlice(token) || headSet.has(head);
};

const lexesToOneVar = (example: string): boolean => {
  const tokens = lex(example);
  return tokens.length === 1 && tokens[0].kind === 'var';
};

type LocaleTable = Record<string, Record<string, unknown> | undefined>;
const varsTable = (locale: typeof en): LocaleTable => ((locale as { vars?: LocaleTable }).vars ?? {});
const LOCALES: [string, LocaleTable][] = [['en', varsTable(en)], ['fr', varsTable(fr)]];

const requiredCopyKeys = (v: VariableDef): string[] => [
  'name',
  'hint',
  'desc',
  ...v.forms.flatMap((form) => (form.chipHint ? [form.chipHint] : []))
];

const missingCopy = (locale: string, table: LocaleTable, v: VariableDef): string[] =>
  requiredCopyKeys(v)
    .filter((key) => !table[v.id]?.[key])
    .map((key) => `${locale}.json vars.${v.id}.${key} is missing`);

const duplicatesIn = (values: string[]): string[] =>
  values.filter((value, index) => values.indexOf(value) !== index);

interface ReplySource {
  where: string;
  tokens: readonly ReplyToken[];
}

const REPLY_SOURCES: ReplySource[] = [
  ...MODULE_CATALOG.flatMap((mod) => mod.replies.map((reply) => ({ where: `${mod.id}.${reply.key}`, tokens: reply.tokens ?? [] }))),
  ...BUILTIN_COMMANDS.map((cmd) => ({ where: `builtin.${cmd.id}`, tokens: cmd.tokens ?? [] }))
];

const leafAt = (tree: unknown, key: string): unknown =>
  key.split('.').reduce<unknown>((node, part) => (node && typeof node === 'object' ? (node as Record<string, unknown>)[part] : undefined), tree);

const hasText = (tree: unknown, key: string): boolean => {
  const leaf = leafAt(tree, key);
  return typeof leaf === 'string' && leaf !== '';
};

const replyNamespace = (hintKey: string): string | undefined => /^replyVars\.(.+)\.[^.]+\.hint$/.exec(hintKey)?.[1];

const claimedNamespace = (source: ReplySource): string | undefined => {
  const hinted = source.tokens.find((token) => token.hintKey);
  return hinted ? replyNamespace(hinted.hintKey!) : undefined;
};

const sortedNames = (names: readonly string[]): string[] => [...new Set(names)].sort();

const replyMismatches = (source: ReplySource): string[] => {
  const ns = claimedNamespace(source);
  if (!ns) return [];
  const goNames = replyTokensGolden.replies[ns];
  if (!goNames) return [`${source.where}: reply_tokens.golden.json has no "${ns}"; add it to app/twitch/sesame/modules/reply_tokens.go and regenerate`];
  const kitNames = sortedNames(source.tokens.map((token) => token.name));
  const same = JSON.stringify(kitNames) === JSON.stringify(sortedNames(goNames));
  return same ? [] : [`${source.where}: kit tokens ${JSON.stringify(kitNames)} differ from Go "${ns}" ${JSON.stringify(sortedNames(goNames))}`];
};

const unresolvedHintKeys = (locale: string, tree: unknown): string[] =>
  REPLY_SOURCES.flatMap((source) =>
    source.tokens
      .filter((token) => token.hintKey && !hasText(tree, token.hintKey))
      .map((token) => `${source.where} token "${token.name}": ${locale}.json has no ${token.hintKey}`)
  );

const IMPORTER_ROOT = join(import.meta.dir, '../importer');

function isImporterSourceName(name: string): boolean {
  if (!name.endsWith('.ts')) return false;
  return !name.endsWith('.test.ts') && !name.endsWith('.d.ts');
}

function importerSourceFiles(dir: string = IMPORTER_ROOT): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === 'testdata') continue;
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...importerSourceFiles(full));
    } else if (isImporterSourceName(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

interface ScannedFile {
  path: string;
  text: string;
}

function bracedLiteralOffenders(file: ScannedFile, targetHeads: Set<string>): string[] {
  const offenders: string[] = [];
  for (const m of file.text.matchAll(/'\{([a-z][a-z.]*)\}'/g)) {
    if (!targetHeads.has(m[1])) offenders.push(`${file.path}: literal '{${m[1]}}' not in TARGETS`);
  }
  return offenders;
}

function emitCallOffenders(file: ScannedFile, targetHeads: Set<string>): string[] {
  const offenders: string[] = [];
  for (const m of file.text.matchAll(/\bemit(?:WithFallback)?\(\s*'([a-z][a-z.]*)'/g)) {
    if (!targetHeads.has(m[1])) offenders.push(`${file.path}: emit('${m[1]}') not in TARGETS`);
  }
  return offenders;
}

const STRING_OR_TEMPLATE = /'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"|`(?:[^`\\]|\\.)*`/g;
const INTERPOLATION = /^\$\{[^{}]*\}/;
const ALLOWLISTED = /\/\/\s*brace-literal-ok:/;

interface ScannedLine {
  file: string;
  line: string;
  index: number;
  lines: string[];
}

function isAllowlisted(ctx: ScannedLine): boolean {
  if (ALLOWLISTED.test(ctx.line)) return true;
  return ctx.index > 0 && ALLOWLISTED.test(ctx.lines[ctx.index - 1]);
}

function bracedStringOffenders(ctx: ScannedLine): string[] {
  if (/^\s*\/\//.test(ctx.line)) return [];
  if (isAllowlisted(ctx)) return [];
  const offenders: string[] = [];
  for (const m of ctx.line.matchAll(STRING_OR_TEMPLATE)) {
    const inner = m[0].slice(1, -1).replace(INTERPOLATION, '');
    if (inner.startsWith('{')) offenders.push(`${ctx.file}:${ctx.index + 1}: ${m[0]}`);
  }
  return offenders;
}

describe('variables parity (engine/scope/testdata/token_catalog.golden.json)', () => {
  test('A: every golden example resolves to a manifest head or alias', () => {
    const uncovered = goldenExamples.filter((example) => !isCovered(example));
    expect(uncovered, 'golden examples with no head or alias in VARIABLES; add them to variables.ts').toEqual([]);
  });

  test('B: every manifest head and alias is answered by the Go resolver', () => {
    const unanswered = VARIABLES.filter((v) => !NOT_RESOLVER_OWNED.has(v.head))
      .flatMap(namesOf)
      .filter((name) => !goldenHeads.has(name) && !goldenAliasHeads.has(name));
    expect(unanswered, 'variables.ts promises names no golden example resolves; check the scope files').toEqual([]);
  });

  test('C: every form example lexes to exactly one variable token', () => {
    const malformed = VARIABLES.flatMap((v) => v.forms.map((form) => ({ id: v.id, example: form.example })))
      .filter((form) => !lexesToOneVar(form.example))
      .map((form) => `${form.id}: ${form.example}`);
    expect(malformed, 'form examples that do not lex to exactly one {…} token').toEqual([]);
  });

  test('C2: every form syntax lexes to exactly one variable token', () => {
    const malformed = VARIABLES.flatMap((v) => v.forms.map((form) => ({ id: v.id, syntax: form.syntax })))
      .filter((form) => !lexesToOneVar(form.syntax))
      .map((form) => `${form.id}: ${form.syntax}`);
    expect(malformed, 'form syntax placeholders that do not lex to exactly one {…} token').toEqual([]);
  });

  test('C3: every alias, braced, lexes to exactly one variable token', () => {
    const malformed = VARIABLES.flatMap((v) => (v.aliases ?? []).map((alias) => ({ id: v.id, span: `{${alias}}` })))
      .filter(({ span }) => !lexesToOneVar(span))
      .map(({ id, span }) => `${id}: ${span}`);
    expect(malformed, 'aliases that do not lex to exactly one {…} token when braced').toEqual([]);
  });

  test('D: every id has name, hint, desc and chip copy in both locales', () => {
    const missing = LOCALES.flatMap(([locale, table]) => VARIABLES.flatMap((v) => missingCopy(locale, table, v)));
    expect(missing).toEqual([]);
  });

  test('E: importer targets resolve to a manifest head or alias', () => {
    const uncovered = Object.values(TARGETS).filter((head) => !isCovered(`{${head}}`));
    expect(uncovered, 'importer targets with no head or alias in VARIABLES').toEqual([]);
  });

  test('E-inverse: every importer-side "{…}" literal resolves through TARGETS', () => {
    const targetHeads = new Set(Object.values(TARGETS));
    const offenders = importerSourceFiles().flatMap((path) => {
      const file: ScannedFile = { path, text: readFileSync(path, 'utf8') };
      return [...bracedLiteralOffenders(file, targetHeads), ...emitCallOffenders(file, targetHeads)];
    });
    expect(offenders, 'importer code minting a span outside TARGETS').toEqual([]);
  });

  test('E-inverse-2: no importer-side string/template literal carries a bare "{" outside TARGETS', () => {
    const files = importerSourceFiles().filter((f) => !f.endsWith(`${sep}targets.ts`));
    const offenders = files.flatMap((file) => {
      const lines = readFileSync(file, 'utf8').split('\n');
      return lines.flatMap((line, index) => bracedStringOffenders({ file, line, index, lines }));
    });
    expect(
      offenders,
      'a string/template literal outside targets.ts that opens with a bare "{" — route it through emit/positional/slice, or allowlist a genuine non-mint with "// brace-literal-ok: <reason>"'
    ).toEqual([]);
  });

  test('F: ids are unique, heads and aliases are unique', () => {
    expect(duplicatesIn(VARIABLES.map((v) => v.id)), 'duplicate ids').toEqual([]);
    expect(duplicatesIn(VARIABLES.flatMap(namesOf)), 'duplicate heads or aliases').toEqual([]);
  });

  test('G: every ReplyToken.hintKey resolves in en and fr', () => {
    expect([...unresolvedHintKeys('en', en), ...unresolvedHintKeys('fr', fr)]).toEqual([]);
  });

  test('H: reply-token inventory matches app/twitch/sesame/modules/reply_tokens.go (Go golden)', () => {
    expect(REPLY_SOURCES.flatMap(replyMismatches)).toEqual([]);
    const claimed = new Set(REPLY_SOURCES.map(claimedNamespace));
    const unclaimed = Object.keys(replyTokensGolden.replies).filter((ns) => !claimed.has(ns));
    expect(unclaimed, 'Go declares reply namespaces no kit replyTokens() call names; fix web/kit/lib/catalog/*.ts').toEqual([]);
  });

  const timerFamilyIds = new Set(golden.surfaces.timer);
  const timerGoldenHeads = sortedNames(
    golden.families
      .filter((family) => timerFamilyIds.has(family.id))
      .flatMap((family) => family.examples)
      .map(tokenHead)
  );
  const nonTimerGoldenHeads = sortedNames(
    golden.families
      .filter((family) => !timerFamilyIds.has(family.id))
      .flatMap((family) => family.examples)
      .map(tokenHead)
  );

  test('I: forSurface(\'timer\') resolves exactly the golden surfaces.timer families', () => {
    const tsTimerHeads = sortedNames(forSurface('timer').map((v) => v.head));
    expect(tsTimerHeads, 'forSurface(\'timer\') / timerOwns must match TimerFamilies() via the golden').toEqual(timerGoldenHeads);
  });

  test('I: forSurface(\'timer\') never resolves a non-timer family\'s head', () => {
    const tsTimerHeadSet = new Set(forSurface('timer').map((v) => v.head));
    const leaked = nonTimerGoldenHeads.filter((head) => tsTimerHeadSet.has(head));
    expect(leaked, 'forSurface(\'timer\') resolves a head from a family timerChain does not mount').toEqual([]);
  });
});
