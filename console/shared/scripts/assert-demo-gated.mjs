#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Source-level demo gate, run BEFORE vite build (and standalone in CI).
//
// assert-production-clean.ts proves demo code is absent from the emitted
// bundle; this proves the source can only ever produce such a bundle. The two
// are complementary: the output scan can only fail after a (slow) build, and
// it cannot explain what to fix. This one names the file and line.
//
// What it enforces:
//   1. Every mention of DEMO is either the canonical gate
//      (`const DEMO = dev && env.DEMO === '1'`) or a use of that local const.
//      Any other shape — a bare `env.DEMO` read, a property read through an
//      aliased env object, a computed/bracket key, a destructure, a bare
//      'DEMO' string — is a runtime switch: a shipped-and-dormant code path an
//      env var can turn back on. Only the gated form folds away at build,
//      because `dev` is a build-time constant.
//   2. Fixture modules are discovered from the code (every dynamic import that
//      looks like fixtures), not from a hardcoded filename, so renaming
//      demo-data.ts to seed.ts cannot slip past.
//   3. Every discovered fixture module carries the sentinel throw, so one that
//      does reach a production build fails loudly instead of serving fixtures.
//   4. Fixture modules are imported dynamically only, from a file that has the
//      canonical gate. A static import is a hard edge into the module graph
//      (the sentinel is a side effect, so Rollup cannot shake it out); an
//      ungated dynamic import is always reachable, so it cannot be shaken out
//      either.
//   5. No stray fixture-shaped module sits unimported under lib/server.
//
// Scanned roots include console/shared, which BOTH consoles bundle
// (`ssr: { noExternal: ['@bagel/shared'] }`) — a fixture or ungated read added
// there would otherwise reach production through an app that never mentions
// DEMO itself.
//
// What it cannot enforce: a demo/preview branch keyed on something other than
// DEMO (a hostname, a header, a feature flag) serving fixtures inlined into an
// ordinary route file. No pattern match can recognize "fabricated data" in
// general. That is the argument for keeping fixtures in their own module and
// the demo path in its own tree, not for adding more tokens here.
import { readdir, readFile, stat } from 'node:fs/promises';
import { dirname, extname, join, relative, resolve, sep } from 'node:path';

const app = process.argv[2];
if (!app) {
  console.error('usage: assert-demo-gated.mjs <app-name>');
  process.exit(2);
}

const appRoot = process.cwd();
const sharedRoot = resolve(appRoot, '..', 'shared');
const sentinel = `${app.toUpperCase()}_DEV_FIXTURE_INCLUDED_IN_PRODUCTION`;
const scanExtensions = new Set(['.ts', '.js', '.mjs', '.svelte']);

// The one sanctioned place the DEMO key is named outside a gate: the boot-time
// refusal that stops a production runtime from pretending to be a demo. It
// reads the key by lookup rather than as a property precisely so it never
// spells the read the output scan bans.
const DEMO_GUARD = resolve(sharedRoot, 'lib', 'server', 'demo-guard.ts');

// Canonical gate. `dev` (SvelteKit's build-time constant) MUST come first:
// `dev && …` folds to `false` before the env is consulted, which is what lets
// Rollup erase the branch.
const GATE = /const\s+DEMO\s*=\s*dev\s*&&\s*(?:process\.)?env\.DEMO\s*===\s*'1'\s*;/;
// Every disallowed way to name the key. Applied after the canonical gate lines
// are removed, so what is left is by definition not the gate.
const PROPERTY_READ = /[.[]\s*['"`]?DEMO\b/;
const STRING_KEY = /['"`]DEMO['"`]/;
const DESTRUCTURE = /\{[^{}]*\bDEMO\b[^{}]*\}\s*=/;
// Matched against path SEGMENTS, not raw substrings: 'ResponseEditor' happens
// to contain the letters of 'seed' ("respon-seEd-itor"), and a substring match
// would flag half the component tree.
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

// Comments describe the gating; they must not be mistaken for it, in either
// direction (a commented-out read is not a violation, a comment saying "dev &&"
// is not a gate).
function stripComments(body) {
  return body.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

// $lib/x -> <app>/src/lib/x, ./x -> sibling, with the extension restored.
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

const fixtureModules = new Map(); // absolute path -> importer
const importedPaths = new Set();

for (const file of files) {
  const name = relative(appRoot, file);
  const raw = await readFile(file, 'utf8');
  const code = stripComments(raw);
  const gated = GATE.test(code);

  // Rule 1 — every DEMO mention is the gate or a use of it.
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
    // Multi-line destructure: the single-line pass above cannot see it.
    if (DESTRUCTURE.test(collapsed) && !raw.split('\n').some((l) => DESTRUCTURE.test(l))) {
      failures.push(`${name}: DEMO is destructured out of the env object across lines, which no build-time constant can fold away`);
    }
  }

  // Rules 2 and 4 — discover fixture modules from imports.
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

// Rule 3 — every discovered fixture module fails loudly if it ever ships.
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

// Rule 5 — no stray fixture-shaped module nobody imports.
for (const file of files) {
  const name = relative(appRoot, file);
  if (!name.includes(`lib${sep}server`)) continue;
  const base = name.split(sep).pop();
  if (!looksLikeFixture(base) || base.startsWith('demo-guard')) continue;
  if (!importedPaths.has(file)) {
    failures.push(`${name}: fixture-shaped module is not reached through a dev-gated dynamic import — delete it or route it through the demo fixture module`);
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
