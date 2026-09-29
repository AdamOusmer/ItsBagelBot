#!/usr/bin/env node
// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { consoleLocales, LOCALES_DIR, readConsoleTree } from '../lib/i18n/tree-fs.ts';

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

function collectLeaves(tree, prefix, out, file) {
  for (const [key, value] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (isLeaf(value)) out.add(path);
    else if (isBranch(value)) collectLeaves(value, path, out, file);
    else fail(`${file}: "${path}" is not a string or array of strings`);
  }
  return out;
}

function parse(locale) {
  try {
    return readConsoleTree(locale);
  } catch (err) {
    return fail(`${locale}: ${err.message}`);
  }
}

function leavesOf(locale) {
  return collectLeaves(parse(locale), '', new Set(), locale);
}

function difference(a, b) {
  return [...a].filter((k) => !b.has(k));
}

function report(locale, missing, extra) {
  if (missing.length) {
    console.warn(`check-i18n: ${locale} console catalog is missing ${missing.length} key(s): ${missing.join(', ')}`);
  }
  if (extra.length) {
    console.warn(`check-i18n: ${locale} console catalog has ${extra.length} extra key(s) absent from ${DEFAULT_LOCALE}: ${extra.join(', ')}`);
  }
  if (!missing.length && !extra.length) {
    console.log(`check-i18n: ${locale} console catalog is in full parity with ${DEFAULT_LOCALE}`);
  }
}

function main() {
  const locales = consoleLocales();
  if (!locales.includes(DEFAULT_LOCALE)) {
    fail(`missing ${DEFAULT_LOCALE}/console in ${LOCALES_DIR}`);
  }
  const enLeaves = leavesOf(DEFAULT_LOCALE);
  for (const locale of locales) {
    if (locale === DEFAULT_LOCALE) continue;
    const leaves = leavesOf(locale);
    report(locale, difference(enLeaves, leaves), difference(leaves, enLeaves));
  }
  console.log(`check-i18n: validated ${locales.length} console catalog(s); ${enLeaves.size} keys in ${DEFAULT_LOCALE}`);
}

main();
