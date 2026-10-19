// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { parseAst } from 'rolldown/parseAst';
import type { AstNode } from './i18n-ast';
import { KeyCatalog } from './i18n-link';
import { alwaysLoadedWith } from './i18n-slices';
import { SliceBuilder } from './i18n-slice-units';
import { analyzeModule } from './i18n-usage';

const HEADER = "import { getI18n, translate } from '@bagel/kit';\n";

function analyze(body: string) {
  const code = HEADER + body;
  return analyzeModule(parseAst(code) as unknown as AstNode, code);
}

const patterns = (body: string) => analyze(body).patterns.map((pattern) => pattern.join('*'));

describe('analyzeModule', () => {
  test('a literal, a conditional and a template name their keys', () => {
    const usage = analyze("function C(on, id) { const { t } = getI18n(); t('a.b'); t(on ? 'a.c' : 'a.d'); t(`vars.${id}.name`); }");
    expect(usage.problems).toEqual([]);
    expect(usage.patterns.map((pattern) => pattern.join('*'))).toEqual(['a.b', 'a.c', 'a.d', 'vars.*.name']);
  });

  test('a key read from a local table that is only read resolves to its strings', () => {
    expect(patterns("function C(k) { const { t } = getI18n(); const KEYS = { x: 'p.x', y: 'p.y' }; return t(KEYS[k]); }")).toEqual(['p.x', 'p.y']);
  });

  test('a table that is written anywhere is not trusted', () => {
    const usage = analyze("function C(k) { const { t } = getI18n(); const KEYS = { x: 'p.x' }; KEYS.x = k; return t(KEYS[k]); }");
    expect(usage.problems).toEqual(['unbounded key: t(KEYS[k])']);
  });

  test('a key from data, a translator handed to an unknown callee or stored, and module-scope calls fail', () => {
    const usage = analyze(
      "const early = translate('en', 'a.b');\nfunction C(row, cb) { const { t } = getI18n(); t(row.key); cb(t); const box = { t }; }"
    );
    expect(usage.problems).toEqual([
      "module-scope translation: translate('en', 'a.b')",
      'unbounded key: t(row.key)',
      'translator escapes: cb(t)',
      'translator escapes: t'
    ]);
  });

  test('a translator passed to an imported function is resolved later through that function', () => {
    const usage = analyze("import { label } from './labels';\nfunction C(def) { const { t } = getI18n(); return label(t, def); }");
    expect(usage.problems).toEqual([]);
    expect(usage.deferred.map(({ kind, source, imported, index }) => [kind, source, imported, index])).toEqual([['call', './labels', 'label', 0]]);
  });

  test('a local wrapper resolves through every direct call and an inline list through its items', () => {
    expect(
      patterns(
        "function C(locale, message) { const tr = (key) => translate(locale, key); tr('w.a'); tr('w.b'); return ['w.c', 'w.d'].some((key) => message === translate(locale, key)); }"
      )
    ).toEqual(['w.a', 'w.b', 'w.c', 'w.d']);
  });

  test('a wrapper that escapes is not resolved', () => {
    const usage = analyze("function C(locale, use) { const tr = (key) => translate(locale, key); use(tr); }");
    expect(usage.problems).toEqual(['unbounded key: translate(locale, key)']);
  });

  test('a key forwarded through an exported function fails, since its callers live in other modules', () => {
    expect(analyze('export function label(locale, key) { return translate(locale, key); }').problems).toEqual(['unbounded key: translate(locale, key)']);
    expect(analyze('export const label = (locale, key) => translate(locale, key);').problems).toEqual(['unbounded key: translate(locale, key)']);
  });

  test('a concatenation with an unresolved side keeps its literal text and widens the rest', () => {
    expect(patterns("function C() { const { t } = getI18n(); const show = (key) => t('ok.' + key); }")).toEqual(['ok.*']);
  });

  test('an i18n export may not be renamed', () => {
    const code = "export { getI18n as useI18n } from './context';";
    expect(analyzeModule(parseAst(code) as unknown as AstNode, code).problems).toEqual(['renamed i18n export: getI18n as useI18n']);
  });
});

describe('KeyCatalog', () => {
  const keys = ['a.b', 'vars.x.name', 'vars.y.name', 'vars.y.hint'];
  test('expands patterns over the English keys and rejects unknown ones', () => {
    const { keys: found, problems } = new KeyCatalog(keys).match([['a.b'], ['vars.', '.name'], ['a.zz'], ['nope.', '']]);
    expect([...found]).toEqual(['a.b', 'vars.x.name', 'vars.y.name']);
    expect(problems).toEqual(['unknown key a.zz', 'no key matches nope.*']);
  });
});

describe('SliceBuilder', () => {
  const chunk = (fileName: string, moduleIds: string[], links: { imports?: string[]; dynamicImports?: string[] } = {}) => ({
    fileName,
    facadeModuleId: moduleIds[0],
    moduleIds,
    imports: links.imports ?? [],
    dynamicImports: links.dynamicImports ?? [],
    isEntry: false
  });
  const node = (index: number) => `/w/.svelte-kit/generated/client-optimized/nodes/${index}.js`;
  const chunks = {
    root: chunk('root', [node(0), 'shell']),
    page: chunk('page', [node(6), 'page'], { imports: ['shared'], dynamicImports: ['editor', 'motion'] }),
    shared: chunk('shared', ['palette']),
    editor: chunk('editor', ['Editor'], { imports: ['shared'] }),
    motion: chunk('motion', ['motion'])
  };
  const keys = new Map<string, Set<string>>([
    ['shell', new Set(['nav.a'])],
    ['page', new Set(['page.title', 'nav.a'])],
    ['palette', new Set(['vars.x'])],
    ['Editor', new Set(['editor.save', 'page.title'])],
    ['motion', new Set(['motion.hint'])]
  ]);
  const builder = new SliceBuilder({ chunks, moduleKeys: keys, ancestors: new Map([[6, new Set([0])]]), units: new Set([node(0), node(6), 'Editor']) });

  test('a lazily imported unit carries its own keys and is not counted in the page that imports it', () => {
    expect([...builder.ownKeys(node(6))].sort()).toEqual(['motion.hint', 'page.title', 'vars.x']);
    expect(builder.contexts('Editor')).toEqual([node(6)]);
    expect([...builder.ownKeys('Editor')]).toEqual(['editor.save']);
  });
});

describe('alwaysLoadedWith', () => {
  test('a node only subtracts what loads with it on every route', () => {
    const loaded = alwaysLoadedWith([
      { id: '/(app)/a', nodes: [2, 6], errors: [3] },
      { id: '/(app)/b/[x]', nodes: [2, 4, 7], errors: [3] },
      { id: '/(public)/c', nodes: [5, 8], errors: [] }
    ]);
    expect([...loaded.get(7)!].sort()).toEqual([0, 2, 4]);
    expect([...loaded.get(4)!].sort()).toEqual([0, 2]);
    expect([...loaded.get(3)!]).toEqual([0]);
    expect([...loaded.get(8)!].sort()).toEqual([0, 5]);
  });
});
