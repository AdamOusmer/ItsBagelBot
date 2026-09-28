// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { existsSync } from 'node:fs';
import { readdir, readFile } from 'node:fs/promises';
import { join, relative, resolve } from 'node:path';

const webRoot = resolve(import.meta.dir, '../..');
const SURFACES = ['marketing/src', 'dashboard/src', 'admin/src', 'docs/src'];

const ELEMENTS = [
  'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'p', 'span', 'small', 'code', 'a', 'button', 'input', 'select', 'textarea', 'table',
];

const ALLOWLIST = new Map([
  [
    'marketing/src/components/home/Header.astro',
    'The hero wordmark. Its `h1` rules are a per-glyph motion rig (three ' +
      'breakpoint clamps, a .line/.glyph split the entrance animation drives), ' +
      'not a type size. The element is the animation. Moving it into the ' +
      'library would ship a landing-page motion to every surface.',
  ],
  [
    'dashboard/src/routes/(public)/login/+page.svelte',
    'The console half of the same hero wordmark, deliberately the same rig so ' +
      'the two front doors match. Same reason: motion, not type.',
  ],
  [
    'marketing/src/components/guides/widgets/PathPicker.astro',
    'A widget that BUILDS its <button> rows in a script and styles them by ' +
      'element because they have no stable class at author time. The rows are ' +
      'a JSON tree view, not controls the button contract covers.',
  ],
  [
    'docs/src/components/MermaidStyles.astro',
    'Dresses the SVG mermaid renders at runtime. Every selector here reaches ' +
      'into a DOM this repo does not author and cannot add class names to. ' +
      'Mermaid names its own nodes, so `pre.mermaid svg … text|span|p` is ' +
      'the only handle there is.',
  ],
  [
    'marketing/src/components/guides/GuideShell.astro',
    'Dresses an authored markdown body (`:global(p:not([class]))` and its ' +
      'siblings). The one case a bare selector is correct: the guide author ' +
      'writes prose, not class names.',
  ],
  [
    'marketing/src/components/legal/LegalShell.astro',
    'Same as GuideShell: the terms and privacy bodies are authored markdown.',
  ],
  [
    'marketing/src/components/builder/CommandBuilder.astro',
    'Builds its palette, variable and recipe rows in a script (document ' +
      'createElement, no class at author time) and styles them by element for ' +
      'the same reason PathPicker does.',
  ],
]);

const PENDING = new Map<string, number>([]);

const HANDCODED =
  'Hand-coded presentation found. A rule that targets a bare element is a second answer to ' +
  '"what does a heading look like"; a rule that targets a .bb-* class reaches into a contract ' +
  'this file does not own. Render the block instead (Heading, Text, Button, Field, Container, ' +
  'Stack, Cluster, Grid, Table …), or add a modifier to the contract in ui/styles/elements/.';

const TYPE_DECL =
  /(^|[\s;{])(font|font-size|font-family|font-weight|line-height|letter-spacing|text-transform|color)\s*:/;

const STYLE_BLOCK = /<style\b[^>]*>([\s\S]*?)<\/style>/g;

type Rule = { selector: string; line: number; body: string };
type Hit = { rel: string; line: number; selector: string; parts: string[] };

const stripComments = (css: string) =>
  css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '));

function readBlock(css: string, start: number): string {
  let depth = 1;
  let body = '';
  for (let i = start; i < css.length && depth > 0; i++) {
    if (css[i] === '{') depth++;
    else if (css[i] === '}') depth--;
    if (depth > 0) body += css[i];
  }
  return body;
}

function* rules(css: string): Generator<Rule> {
  const clean = stripComments(css);
  let line = 1;
  let headStart = 0;
  for (const match of clean.matchAll(/[{};\n]/g)) {
    const at = match.index;
    if (match[0] === '\n') {
      line++;
      continue;
    }
    const selector = clean.slice(headStart, at).trim();
    headStart = at + 1;
    if (match[0] !== '{' || !selector || selector.startsWith('@')) continue;
    yield { selector, line, body: readBlock(clean, at + 1) };
  }
}

export function offendingParts(selector: string, body: string): string[] {
  const flat = selector.replace(/:global\(([^)]*)\)/g, ' $1 ');
  const hits = new Set<string>();
  for (const cls of flat.matchAll(/\.(bb-[\w-]+)/g)) hits.add(`.${cls[1]}`);
  if (!TYPE_DECL.test(body)) return [...hits];
  const withoutAttrs = flat.replace(/\[[^\]]*\]/g, ' ');
  for (const match of withoutAttrs.matchAll(/(^|[\s>+~,()])([a-z][a-z0-9]*)\b/g)) {
    if (ELEMENTS.includes(match[2])) hits.add(match[2]);
  }
  return [...hits];
}

export function offendersIn(rel: string, source: string): Hit[] {
  const hits: Hit[] = [];
  for (const block of source.matchAll(STYLE_BLOCK)) {
    const before = source.slice(0, block.index).split('\n').length - 1;
    for (const { selector, line, body } of rules(block[1])) {
      const parts = offendingParts(selector, body);
      if (parts.length) hits.push({ rel, line: before + line, selector: selector.replace(/\s+/g, ' '), parts });
    }
  }
  return hits;
}

async function* components(dir: string): AsyncGenerator<string> {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory() && entry.name !== 'node_modules' && entry.name !== '.astro') yield* components(full);
    else if (entry.name.endsWith('.svelte') || entry.name.endsWith('.astro')) yield full;
  }
}

async function scan(): Promise<Map<string, Hit[]>> {
  const byFile = new Map<string, Hit[]>();
  for (const surface of SURFACES) {
    for await (const file of components(join(webRoot, surface))) {
      const rel = relative(webRoot, file);
      if (ALLOWLIST.has(rel)) continue;
      const hits = offendersIn(rel, await readFile(file, 'utf8'));
      if (hits.length) byFile.set(rel, hits);
    }
  }
  return byFile;
}

describe('offendingParts', () => {
  test('flags a bare element that sets type', () => {
    expect(offendingParts('.head h2', 'font-size: 18px;')).toEqual(['h2']);
  });

  test('ignores a bare element that only lays out', () => {
    expect(offendingParts('.row span', 'display: flex; gap: 4px;')).toEqual([]);
  });

  test('flags any .bb-* class, including inside :global()', () => {
    expect(offendingParts('.editor :global(.bb-field)', 'margin: 0;')).toEqual(['.bb-field']);
  });

  test('does not read element names inside attribute selectors', () => {
    expect(offendingParts('[data-kind="span"] .label', 'color: red;')).toEqual([]);
  });

  test('reports the line of each offending rule in a component', () => {
    const source = '<div></div>\n<style>\n  .ok { display: grid; }\n  .title h2 { font-weight: 700; }\n</style>\n';
    expect(offendersIn('x.svelte', source)).toEqual([
      { rel: 'x.svelte', line: 4, selector: '.title h2', parts: ['h2'] },
    ]);
  });
});

describe('components render blocks instead of hand-coded presentation', () => {
  test('no offending rule outside the allowlist and pending budget', async () => {
    const byFile = await scan();
    const offenders = [...byFile].flatMap(([rel, hits]) =>
      hits.slice(PENDING.get(rel) ?? 0).map((h) => `${h.rel}:${h.line}  ${h.selector}   [${h.parts.join(' ')}]`),
    );
    expect(offenders, HANDCODED).toEqual([]);
  });

  test('a paid-down pending budget is lowered in the same commit', async () => {
    const byFile = await scan();
    const paid = [...PENDING]
      .filter(([rel, budget]) => (byFile.get(rel)?.length ?? 0) < budget)
      .map(([rel, budget]) => `${rel}: ${budget} -> ${byFile.get(rel)?.length ?? 0}`);
    expect(paid, 'Lower these PENDING numbers in web/kit/scripts/blocks-not-handcoded.test.ts').toEqual([]);
  });

  test('every allowlisted file still exists', () => {
    const missing = [...ALLOWLIST.keys()].filter((rel) => !existsSync(join(webRoot, rel)));
    expect(missing, 'Remove allowlist entries for deleted files').toEqual([]);
  });
});
