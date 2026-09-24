#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdir, readFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';

const kitRoot = resolve(import.meta.dir, '..');
const componentsDir = join(kitRoot, 'components');

const ALLOWLIST = new Map([
  [
    'Bolota.svelte',
    'wraps the third-party @luzir/bolota engine and its own class namespace',
  ],
]);

async function componentFiles() {
  const entries = await readdir(componentsDir, { withFileTypes: true });
  return entries
    .filter((e) => e.isFile() && e.name.endsWith('.svelte'))
    .map((e) => e.name)
    .sort();
}

const STYLE_BLOCK = /^\s*<style\b/m;

function parseSkips() {
  const fromArgs = process.argv
    .slice(2)
    .filter((a) => a.startsWith('--skip='))
    .flatMap((a) => a.slice('--skip='.length).split(','));
  const fromEnv = (process.env.KIT_UI_ONLY_SKIP ?? '').split(',');
  return new Set(
    [...fromArgs, ...fromEnv]
      .map((s) => s.trim())
      .filter(Boolean)
      .map((s) => (s.endsWith('.svelte') ? s : `${s}.svelte`)),
  );
}

const skips = parseSkips();
const offenders = [];
const skipped = [];

for (const name of await componentFiles()) {
  if (ALLOWLIST.has(name)) continue;
  const source = await readFile(join(componentsDir, name), 'utf8');
  if (!STYLE_BLOCK.test(source)) continue;

  const line = source.split('\n').findIndex((l) => /^\s*<style\b/.test(l)) + 1;
  if (skips.has(name)) {
    skipped.push(`${name}:${line}`);
    continue;
  }
  offenders.push(`web/kit/components/${name}:${line}`);
}

if (skipped.length > 0) {
  console.log(
    `assert-ui-only-in-ui: ${skipped.length} still carrying presentation, owned by an unlanded branch of the element stack: ${skipped.join(', ')}`,
  );
}

if (offenders.length > 0) {
  console.error(
    'assert-ui-only-in-ui: presentation found in a kit component.\n' +
      'A kit component binds data and renders a @bagel/ui element; what it looks\n' +
      'like belongs in ui/styles/elements/<name>.css with an adapter beside it.\n',
  );
  for (const o of offenders) console.error(`  ${o}  <style> block`);
  console.error(
    `\n${offenders.length} component${offenders.length === 1 ? '' : 's'} with a <style> block.` +
      `\nAllowlisted, with the reason, in ${'web/kit/scripts/assert-ui-only-in-ui.mjs'}:` +
      `\n${[...ALLOWLIST.entries()].map(([k, why]) => `  ${k} — ${why}`).join('\n')}`,
  );
  process.exit(1);
}

console.log(
  `assert-ui-only-in-ui: OK (${ALLOWLIST.size} allowlisted, ${skipped.length} skipped)`,
);
