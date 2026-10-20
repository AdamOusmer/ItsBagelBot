// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { parseAst } from 'rolldown/parseAst';
import type { AstNode } from './i18n-ast';
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
