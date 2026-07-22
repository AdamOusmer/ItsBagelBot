#!/usr/bin/env node
// i18n parity gate, run before each app's `vite build`. fs-only (no app imports)
// so it works in the Containerfile build stage before Vite has run.
//
// Fails the build (exit 1) ONLY on structural problems that would ship a broken
// catalog: a missing en.json, unparseable JSON, or a leaf that is not a string
// or an array of strings (with the offending file + JSON path). Key gaps between
// locales are reported as warnings and NEVER fail the build — a missing key
// falls back to English at runtime, so a partial translation can ship safely.
import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const DIR = join(here, '../lib/i18n/locales');
const DEFAULT_LOCALE = 'en';

function fail(msg) {
  console.error(`check-i18n: ${msg}`);
  process.exit(1);
}

function isStringArray(v) {
  return Array.isArray(v) && v.every((x) => typeof x === 'string');
}

function isLeaf(v) {
  return typeof v === 'string' || isStringArray(v);
}

function isBranch(v) {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

// Collect leaf dot-paths into `out`; fail() on any leaf that is not a string or
// an array of strings.
function collectLeaves(tree, prefix, out, file) {
  for (const [key, value] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (isLeaf(value)) out.add(path);
    else if (isBranch(value)) collectLeaves(value, path, out, file);
    else fail(`${file}: "${path}" is not a string or array of strings`);
  }
  return out;
}

function parse(file) {
  try {
    return JSON.parse(readFileSync(join(DIR, file), 'utf8'));
  } catch (err) {
    return fail(`${file}: invalid JSON — ${err.message}`);
  }
}

function leavesOf(file) {
  return collectLeaves(parse(file), '', new Set(), file);
}

function difference(a, b) {
  return [...a].filter((k) => !b.has(k));
}

function report(locale, missing, extra) {
  if (missing.length) {
    console.warn(`check-i18n: ${locale}.json is missing ${missing.length} key(s): ${missing.join(', ')}`);
  }
  if (extra.length) {
    console.warn(`check-i18n: ${locale}.json has ${extra.length} extra key(s) absent from ${DEFAULT_LOCALE}.json: ${extra.join(', ')}`);
  }
  if (!missing.length && !extra.length) {
    console.log(`check-i18n: ${locale}.json is in full parity with ${DEFAULT_LOCALE}.json`);
  }
}

function main() {
  const files = readdirSync(DIR).filter((f) => f.endsWith('.json')).sort();
  if (!files.includes(`${DEFAULT_LOCALE}.json`)) {
    fail(`missing ${DEFAULT_LOCALE}.json in ${DIR}`);
  }
  const enLeaves = leavesOf(`${DEFAULT_LOCALE}.json`);
  for (const file of files) {
    const locale = file.slice(0, -'.json'.length);
    if (locale === DEFAULT_LOCALE) continue;
    const leaves = leavesOf(file);
    report(locale, difference(enLeaves, leaves), difference(leaves, enLeaves));
  }
  console.log(`check-i18n: validated ${files.length} locale file(s); ${enLeaves.size} keys in ${DEFAULT_LOCALE}.json`);
}

main();
