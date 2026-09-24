#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
      'not a type size — the element is the animation. Moving it into the ' +
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
      'into a DOM this repo does not author and cannot add class names to — ' +
      'mermaid names its own nodes — so `pre.mermaid svg … text|span|p` is ' +
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

const PENDING_ROWS = [
];

const TYPE_DECL =
  /(^|[\s;{])(font|font-size|font-family|font-weight|line-height|letter-spacing|text-transform|color)\s*:/;

const PENDING = new Map(PENDING_ROWS);

const STYLE_BLOCK = /<style\b[^>]*>([\s\S]*?)<\/style>/g;

const stripComments = (css) =>
  css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '));

function readBlock(css, start) {
  let depth = 1;
  let body = '';
  for (let i = start; i < css.length && depth > 0; i++) {
    if (css[i] === '{') depth++;
    else if (css[i] === '}') depth--;
    if (depth > 0) body += css[i];
  }
  return body;
}

function advance(css, match, scan) {
  const token = match[0];
  const at = match.index;
  if (token === '\n') {
    scan.line++;
    return null;
  }
  if (token !== '{') {
    scan.depth -= token === '}' ? 1 : 0;
    scan.headStart = at + 1;
    return null;
  }
  scan.depth++;
  const selector = css.slice(scan.headStart, at).trim();
  scan.headStart = at + 1;
  if (!selector || selector.startsWith('@')) return null;
  return { selector, line: scan.line, body: readBlock(css, at + 1) };
}

function* rules(css) {
  const clean = stripComments(css);
  const scan = { line: 1, headStart: 0, depth: 0 };
  for (const match of clean.matchAll(/[{};\n]/g)) {
    const hit = advance(clean, match, scan);
    if (hit) yield hit;
  }
}

function offendingParts(selector, body) {
  const flat = selector.replace(/:global\(([^)]*)\)/g, ' $1 ');
  const hits = new Set();
  for (const cls of flat.matchAll(/\.(bb-[\w-]+)/g)) hits.add(`.${cls[1]}`);
  if (TYPE_DECL.test(body)) {
    const withoutAttrs = flat.replace(/\[[^\]]*\]/g, ' ');
    for (const match of withoutAttrs.matchAll(/(^|[\s>+~,()])([a-z][a-z0-9]*)\b/g)) {
      if (ELEMENTS.includes(match[2])) hits.add(match[2]);
    }
  }
  return [...hits];
}

async function* walk(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === 'node_modules' || entry.name === '.astro') continue;
      yield* walk(full);
      continue;
    }
    if (entry.name.endsWith('.svelte') || entry.name.endsWith('.astro')) yield full;
  }
}

const byFile = new Map();
let scanned = 0;

for (const surface of SURFACES) {
  const dir = join(webRoot, surface);
  for await (const file of walk(dir)) {
    const rel = relative(webRoot, file);
    if (ALLOWLIST.has(rel)) continue;
    const source = await readFile(file, 'utf8');
    scanned++;
    for (const block of source.matchAll(STYLE_BLOCK)) {
      const before = source.slice(0, block.index).split('\n').length - 1;
      for (const { selector, line, body } of rules(block[1])) {
        const parts = offendingParts(selector, body);
        if (!parts.length) continue;
        if (!byFile.has(rel)) byFile.set(rel, []);
        byFile.get(rel).push({
          line: before + line,
          selector: selector.replace(/\s+/g, ' '),
          parts,
        });
      }
    }
  }
}

const offenders = [];
const paid = [];
let debt = 0;

for (const [rel, found] of byFile) {
  const budget = PENDING.get(rel) ?? 0;
  debt += Math.min(found.length, budget);
  if (found.length <= budget) continue;
  for (const hit of found.slice(budget)) offenders.push({ rel, ...hit, budget, found: found.length });
}

for (const [rel, budget] of PENDING) {
  const now = byFile.get(rel)?.length ?? 0;
  if (now < budget) paid.push(`${rel}: ${budget} -> ${now}`);
}

if (paid.length > 0) {
  console.error(
    'assert-blocks-not-handcoded: debt was paid down and the ratchet was not\n' +
      'lowered with it. Update these PENDING numbers in\n' +
      'web/kit/scripts/assert-blocks-not-handcoded.mjs in the same commit:\n',
  );
  for (const p of paid) console.error(`  ${p}`);
  process.exit(1);
}

if (offenders.length > 0) {
  console.error(
    'assert-blocks-not-handcoded: hand-coded presentation found.\n' +
      'A rule that targets a bare element is a second answer to "what does a\n' +
      'heading look like"; a rule that targets a .bb-* class reaches into a\n' +
      'contract this file does not own. Render the block instead (Heading, Text,\n' +
      'Button, Field, Container, Stack, Cluster, Grid, Table …), or add a\n' +
      'modifier to the contract in ui/styles/elements/.\n',
  );
  for (const o of offenders) {
    console.error(
      `  ${o.rel}:${o.line}  ${o.selector}   [${o.parts.join(' ')}]` +
        (o.budget ? `   (file allowed ${o.budget}, has ${o.found})` : ''),
    );
  }
  console.error(
    `\n${offenders.length} rule${offenders.length === 1 ? '' : 's'} across ${
      new Set(offenders.map((o) => o.rel)).size
    } file${new Set(offenders.map((o) => o.rel)).size === 1 ? '' : 's'}.` +
      `\nAllowlisted, with the reason, in web/kit/scripts/assert-blocks-not-handcoded.mjs:` +
      `\n${[...ALLOWLIST.entries()].map(([k, why]) => `  ${k}\n    ${why}`).join('\n')}`,
  );
  process.exit(1);
}

console.log(
  `assert-blocks-not-handcoded: OK (${scanned} components scanned, ` +
    `${ALLOWLIST.size} allowlisted, ${debt} rules of counted debt in ${PENDING.size} files)`,
);
