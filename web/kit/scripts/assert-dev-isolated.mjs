#!/usr/bin/env bun
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readdir, readFile, stat } from 'node:fs/promises';
import { dirname, extname, join, relative, resolve, sep } from 'node:path';

const [app, ...devRoutes] = process.argv.slice(2);
if (!app) {
  console.error('usage: assert-dev-isolated.mjs <app-name> [dev-only route fragment...]');
  process.exit(2);
}

const appRoot = process.cwd();
const kitRoot = resolve(appRoot, '..', 'kit');
const devRoot = join(appRoot, 'src', 'dev');
const scanExtensions = new Set(['.ts', '.js', '.mjs', '.svelte']);

const DEMO_READ = /\benv\b[^\n]*\bDEMO\b|['"`]DEMO['"`]|\bDEMO_PLAN\b|\bconst\s+DEMO\s*=/;
const IMPORT = /(?:\bfrom\s*|\bimport\s*\(\s*|^\s*import\s+)['"]([^'"]+)['"]/gm;
const FIXTURE_WORDS = new Set(['demo', 'fixture', 'fixtures', 'sample', 'seed', 'mock']);

async function filesUnder(dir) {
  const entries = await readdir(dir, { withFileTypes: true }).catch(() => []);
  const nested = await Promise.all(
    entries.map((entry) => {
      const path = join(dir, entry.name);
      return entry.isDirectory() ? filesUnder(path) : Promise.resolve([path]);
    })
  );
  return nested.flat().filter((file) => scanExtensions.has(extname(file)));
}

function stripComments(body) {
  return body.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

function resolveSpecifier(specifier, fromFile) {
  if (specifier.startsWith('#lib/')) return join(appRoot, 'src', 'lib', specifier.slice('#lib/'.length));
  if (specifier.startsWith('.')) return resolve(dirname(fromFile), specifier);
  return null;
}

const isFixtureName = (file) =>
  (file.split(sep).pop() ?? '')
    .replace(/\.[a-z]+$/i, '')
    .split(/[-_.]/)
    .some((part) => FIXTURE_WORDS.has(part.toLowerCase()));

const isDevRoute = (file) => devRoutes.some((fragment) => file.includes(fragment));

function importsIntoDev(code, file) {
  return [...code.matchAll(IMPORT)].map((m) => resolveSpecifier(m[1], file)).filter((target) => target?.startsWith(devRoot + sep));
}

async function productionFailures(file) {
  const name = relative(appRoot, file);
  const code = stripComments(await readFile(file, 'utf8'));
  const failures = [];
  if (DEMO_READ.test(code)) failures.push(`${name}: reads a DEMO flag; dev fixtures load only through src/dev under vite dev`);
  if (!isDevRoute(file) && importsIntoDev(code, file).length > 0) {
    failures.push(`${name}: imports from src/dev, which production builds must never reach`);
  }
  if (isFixtureName(file)) failures.push(`${name}: fixture-shaped module outside src/dev`);
  return failures;
}

async function exists(path) {
  return stat(path).then((s) => s.isFile(), () => false);
}

async function overrideFailures(file) {
  const rel = relative(devRoot, file);
  const [head, ...rest] = rel.split(sep);
  if (rest.length === 0 || head === 'wire') return [];
  const target = head === 'kit' ? join(kitRoot, 'lib', ...rest) : join(appRoot, 'src', 'lib', rel);
  return (await exists(target)) ? [] : [`${relative(appRoot, file)}: overrides ${relative(appRoot, target)}, which does not exist`];
}

const productionRoots = [join(appRoot, 'src'), join(kitRoot, 'lib'), join(kitRoot, 'components')];
const productionFiles = (await Promise.all(productionRoots.map(filesUnder))).flat().filter((file) => !file.startsWith(devRoot + sep) && !file.endsWith('.test.ts'));
const devFiles = await filesUnder(devRoot);

const failures = [
  ...(await Promise.all(productionFiles.map(productionFailures))).flat(),
  ...(await Promise.all(devFiles.map(overrideFailures))).flat()
];

if (failures.length > 0) {
  console.error(`${app}: dev runtime code leaks outside src/dev:`);
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`Verified ${productionFiles.length} ${app} + kit source files stay clear of src/dev, and ${devFiles.length} dev files map to real modules.`);
