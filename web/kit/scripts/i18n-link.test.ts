// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { parseAst } from 'rolldown/parseAst';
import type { AstNode } from './i18n-ast';
import { Linker } from './i18n-link';
import { analyzeModule } from './i18n-usage';

const QUOTES = '/app/routes/quotes.ts';
const LABEL = '/app/lib/zz-label.ts';

function link(label: string, call: string) {
  const files: Record<string, string> = {
    [QUOTES]: `import { getI18n } from '@bagel/kit';\nimport { label } from './zz-label';\nfunction Quotes() { const { t } = getI18n(); return ${call}; }`,
    [LABEL]: label
  };
  const parse = (code: string) => parseAst(code) as unknown as AstNode;
  const linker = new Linker({
    resolve: async (source) => (source === './zz-label' ? LABEL : undefined),
    code: (id) => files[id],
    exports: () => ['label'],
    parse
  });
  return linker.link(QUOTES, analyzeModule(parse(files[QUOTES]), files[QUOTES]));
}

describe('Linker', () => {
  test('a translator handed to an exported helper that forwards its key parameter fails the link', async () => {
    const linked = await link('export function label(t, key) { return t(key); }', "label(t, 'import.errTimeout')");
    expect(linked.patterns).toEqual([]);
    expect(linked.problems).toEqual(["unbounded key: t(key) (via label(t, 'import.errTimeout'))"]);
  });

  test('a translator handed to an exported helper with literal keys resolves through it', async () => {
    const linked = await link("export function label(t, on) { return t(on ? 'quotes.on' : 'quotes.off'); }", 'label(t, true)');
    expect(linked.problems).toEqual([]);
    expect(linked.patterns).toEqual([['quotes.on'], ['quotes.off']]);
  });

  test('a key table imported from another module must be a frozen literal', async () => {
    const frozen = await link("export const label = Object.freeze({ a: 'quotes.a' });", 't(label.a)');
    const open = await link("export const label = { a: 'quotes.a' };", 't(label.a)');
    expect([frozen.patterns, frozen.problems]).toEqual([[['quotes.a']], []]);
    expect(open.problems).toEqual(['imported key table is not a frozen literal of strings: label.a']);
  });
});
