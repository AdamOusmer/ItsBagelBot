// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Every t('some.key') literal in the console must resolve to a real string.
//
// Nothing enforced this before. keys.d.ts is generated from the locales and
// looks like a type guard, but translate() takes `key: string`, so a call site
// naming a key that does not exist type-checks fine and lookup() falls back to
// returning the key itself. The failure is therefore invisible in CI and
// visible only as raw "billing.premiumFeat5" text on the page, which is how
// exactly that shipped: a key was dropped from the locale while its call site
// stayed.
//
// Only literal keys are checked. Dynamic lookups (ERROR_SLUG_KEYS[slug] and
// friends) are indexed through typed maps whose VALUES are literals elsewhere
// in the source, so they are covered where they are declared rather than where
// they are used.

import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { extname, join } from 'node:path';
import en from './locales/en.json';

const ROOTS = [
  join(import.meta.dir, '../../../dashboard/src'),
  join(import.meta.dir, '../../../admin/src'),
  join(import.meta.dir, '../..')
];
const EXT = new Set(['.svelte', '.ts']);
const SKIP = new Set(['node_modules', '.svelte-kit', 'build', 'dist', 'locales']);

function sourceFiles(dir: string): string[] {
  let out: string[] = [];
  for (const name of readdirSync(dir)) {
    if (SKIP.has(name)) continue;
    const full = join(dir, name);
    if (statSync(full).isDirectory()) out = out.concat(sourceFiles(full));
    else if (EXT.has(extname(name)) && !full.endsWith('.test.ts')) out.push(full);
  }
  return out;
}

function resolves(key: string): boolean {
  let node: unknown = en;
  for (const part of key.split('.')) {
    if (node === null || typeof node !== 'object') return false;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === 'string' || Array.isArray(node);
}

// t('a.b') / t("a.b") / t(`a.b`), optionally with params after the key.
const CALL = /\bt\(\s*['"`]([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)+)['"`]/g;

// Keys that were ALREADY missing when this guard was written. They render as
// raw "spotify.queueTitle" text on the Song Requests, Quotes and Import pages
// today. They are listed rather than fixed here because each one is a piece of
// product copy in two languages, and inventing that silently is worse than
// naming the backlog.
//
// This list may only shrink. A key that starts resolving must be deleted from
// here (the second test enforces that), so the baseline cannot quietly become
// a permanent excuse.
const KNOWN_MISSING = new Set([
  'import.errWrongType',
  'quotes.editBtnShort',
  'quotes.inspector',
  'quotes.inspectorIdle',
  'quotes.listLabel',
  'quotes.quoteDetails',
  'spotify.liveOnlyLabel',
  'spotify.queueAskedBy',
  'spotify.queueEmpty',
  'spotify.queueNow',
  'spotify.queueRefresh',
  'spotify.queueTitle',
  'spotify.quotaHelp',
  'spotify.quotaSave',
  'spotify.quotaSaveFailed',
  'spotify.quotaSaved',
  'spotify.quotaTitle',
  'spotify.quotaUnlimited',
  'spotify.reconnectSpotify',
  'spotify.redeemLiveOnlyOff',
  'spotify.redeemLiveOnlyOn',
  'spotify.scopeGap',
  'spotify.srLiveOnlyOff',
  'spotify.srLiveOnlyOn',
]);

describe('i18n literal keys', () => {
  test('every t() literal resolves in en.json', () => {
    const missing: string[] = [];
    for (const root of ROOTS) {
      for (const file of sourceFiles(root)) {
        const src = readFileSync(file, 'utf8');
        for (const m of src.matchAll(CALL)) {
          if (!resolves(m[1]) && !KNOWN_MISSING.has(m[1])) {
            missing.push(`${m[1]}  (${file.split('/src/').pop() ?? file})`);
          }
        }
      }
    }
    expect(missing).toEqual([]);
  });

  // The baseline must shrink, never linger. Once a key is given a string, it
  // has to leave this list or the list stops meaning "known broken".
  test('the known-missing baseline holds no keys that now resolve', () => {
    const fixed = [...KNOWN_MISSING].filter(resolves);
    expect(fixed).toEqual([]);
  });

  // The key that motivated this test, pinned by name: it is a plan-comparison
  // bullet, so losing it again silently downgrades what Premium appears to buy.
  test('the premium feature list is complete', () => {
    for (const n of [1, 2, 3, 4, 5]) {
      expect(resolves(`billing.premiumFeat${n}`)).toBe(true);
    }
  });
});
