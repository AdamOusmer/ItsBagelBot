#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdir, readFile, stat } from 'node:fs/promises';
import { dirname, extname, join, relative, resolve, sep } from 'node:path';

const app = process.argv[2];
if (!app) {
  console.error('usage: assert-demo-gated.mjs <app-name>');
  process.exit(2);
}

const appRoot = process.cwd();
const sharedRoot = resolve(appRoot, '..', 'kit');
const sentinel = `${app.toUpperCase()}_DEV_FIXTURE_INCLUDED_IN_PRODUCTION`;
const scanExtensions = new Set(['.ts', '.js', '.mjs', '.svelte']);

const DEMO_GUARD = resolve(sharedRoot, 'lib', 'server', 'demo-guard.ts');

const GATE = /const\s+DEMO\s*=\s*dev\s*&&\s*(?:process\.)?env\.DEMO\s*===\s*'1'\s*;/;
const PROPERTY_READ = /[.[]\s*['"`]?DEMO\b/;
const STRING_KEY = /['"`]DEMO['"`]/;
const DESTRUCTURE = /\{[^{}]*\bDEMO\b[^{}]*\}\s*=/;
const FIXTURE_WORDS = new Set(['demo', 'fixture', 'sample', 'seed', 'mock']);

function looksLikeFixture(specifier) {
  const base = (specifier.split(/[/\\]/).pop() ?? '').replace(/\.[a-z]+$/i, '');
  return base
    .split(/[-_.]/)
    .some((part) => FIXTURE_WORDS.has(part.toLowerCase().replace(/s$/, '')));
}
const DYNAMIC_IMPORT = /\bimport\(\s*['"]([^'"]+)['"]\s*\)/g;
const STATIC_IMPORT = /^\s*(?:import|export)\b[^\n]*from\s*['"]([^'"]+)['"]/;

async function filesUnder(dir) {
  const entries = await readdir(dir, { withFileTypes: true }).catch(() => []);
  const nested = await Promise.all(
    entries.map((entry) => {
      const path = `${dir}${sep}${entry.name}`;
      return entry.isDirectory() ? filesUnder(path) : Promise.resolve([path]);
    })
  );
  return nested.flat();
}

function stripComments(body) {
  return body.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

async function resolveSpecifier(specifier, fromFile) {
  const base = specifier.startsWith('$lib/')
    ? join(appRoot, 'src', 'lib', specifier.slice('$lib/'.length))
    : specifier.startsWith('.')
      ? resolve(dirname(fromFile), specifier)
      : null;
  if (!base) return null;
  for (const candidate of [base, `${base}.ts`, `${base}.js`, join(base, 'index.ts')]) {
    if (await stat(candidate).then((s) => s.isFile(), () => false)) return candidate;
  }
  return base;
}

const failures = [];
const roots = [join(appRoot, 'src'), join(appRoot, 'static'), join(sharedRoot, 'lib'), join(sharedRoot, 'components')];
const files = (await Promise.all(roots.map(filesUnder))).flat().filter((f) => scanExtensions.has(extname(f)));

const fixtureModules = new Map();
const importedPaths = new Set();

for (const file of files) {
  const name = relative(appRoot, file);
  const raw = await readFile(file, 'utf8');
  const code = stripComments(raw);
  const gated = GATE.test(code);

  if (file !== DEMO_GUARD) {
    const withoutGate = code.replace(new RegExp(GATE.source, 'g'), '');
    const collapsed = withoutGate.replace(/\s+/g, ' ');
    raw.split('\n').forEach((line, i) => {
      const clean = stripComments(line).replace(new RegExp(GATE.source), '');
      if (PROPERTY_READ.test(clean) || STRING_KEY.test(clean) || DESTRUCTURE.test(clean)) {
        failures.push(
          `${name}:${i + 1}: DEMO is read outside the canonical \`const DEMO = dev && env.DEMO === '1'\` gate, so the branch survives into production builds`
        );
      }
    });
    if (DESTRUCTURE.test(collapsed) && !raw.split('\n').some((l) => DESTRUCTURE.test(l))) {
      failures.push(`${name}: DEMO is destructured out of the env object across lines, which no build-time constant can fold away`);
    }
  }

  for (const [, specifier] of code.matchAll(DYNAMIC_IMPORT)) {
    if (!looksLikeFixture(specifier)) continue;
    const target = await resolveSpecifier(specifier, file);
    if (!target) continue;
    importedPaths.add(target);
    fixtureModules.set(target, name);
    if (!gated) {
      failures.push(`${name}: imports the fixture module ${specifier} without the \`const DEMO = dev && …\` gate in the same file, so the import is always reachable and cannot be tree-shaken`);
    }
  }
  code.split('\n').forEach((line, i) => {
    const m = line.match(STATIC_IMPORT);
    if (m && looksLikeFixture(m[1]) && !m[1].includes('demo-guard')) {
      failures.push(`${name}:${i + 1}: fixture module ${m[1]} is imported statically; use \`await import(…)\` inside the dev-gated branch`);
    }
  });
}

for (const [path, importer] of fixtureModules) {
  const body = await readFile(path, 'utf8').catch(() => null);
  if (body === null) {
    failures.push(`${relative(appRoot, path)}: imported by ${importer} but does not exist`);
    continue;
  }
  if (!body.includes(`throw new Error('${sentinel}')`)) {
    failures.push(`${relative(appRoot, path)}: missing the \`if (!dev) throw new Error('${sentinel}')\` sentinel`);
  }
}

for (const file of files) {
  const name = relative(appRoot, file);
  if (!name.includes(`lib${sep}server`)) continue;
  const base = name.split(sep).pop();
  if (!looksLikeFixture(base) || base.startsWith('demo-guard')) continue;
  if (!importedPaths.has(file)) {
    failures.push(`${name}: fixture-shaped module is not reached through a dev-gated dynamic import: delete it or route it through the demo fixture module`);
  }
}

if (failures.length > 0) {
  console.error(`${app}: demo code is not fully gated behind the build-time \`dev\` constant:`);
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(
  `Verified ${files.length} ${app} + shared source files: every DEMO mention is the build-time gate, and ${fixtureModules.size} fixture module(s) are sentinel-guarded behind dev-gated dynamic imports.`
);
