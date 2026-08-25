// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// MOD's values are not a dashboard-private vocabulary: each one is the
// ModuleView key a sesame module reads its Configs blob out of, so the real
// contract runs across languages (app/sesame/modules/*.go and, for timers,
// app/sesame/engine/timers_valkey.go). The TypeScript-side test in
// ../module-catalog.test.ts only proves MOD and the catalog agree with each
// other — both sides could agree on a name Go stopped using, and the symptom
// is silent: upsertModule writes a row nothing reads, the feature is simply
// dead for every broadcaster who toggles it.
//
// So this test reads the Go source. Two alternatives were rejected: a Go test
// reading the .ts files inverts the dependency (the Go service would fail on a
// dashboard edit), and a generated JSON manifest is another artifact that can
// go stale between the two. Parsing is a regex over two declaration shapes,
// which is enough because the module package has exactly two: a
// `const <x>ModuleName = "<name>"` and the literal/const first argument to
// module.NewModule. A third shape shows up as an empty Go set, and the
// assertions below fail loudly rather than passing vacuously.

import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { MOD } from './index';

const SESAME = join(import.meta.dir, '../../../../app/sesame');
const GO_DIRS = ['modules', 'engine'];

// Catalog ids with no sesame module behind them, and why. Anything else in MOD
// must name a module Go actually serves.
const NO_GO_MODULE: Record<string, string> = {
  counters: 'a catalog tool, not a module: counters are rows on the loyalty service, and the tile carries toggleable:false so no ModuleView row is ever written'
};

// Named Go modules the dashboard deliberately does not surface as a tile yet.
// Shrinking this list is the point: a module lands in Go, the dashboard owes it
// a catalog entry, and this test is what remembers.
const NOT_IN_DASHBOARD: Record<string, string> = {
  moderation: 'the mod-facing command set, configured through automod rather than its own tile'
};

// Two regexes for the module package's two declaration shapes, hoisted so the
// readers below stay a single flat expression each: nested loop-and-branch
// bodies here are what CodeScene flags as Bumpy Road, and a parser that grows
// a third shape should become a third named reader rather than another branch.
const CONST_DECL = /(\w*[Mm]oduleName)\s*=\s*"([a-z]+)"/g;
const NEW_MODULE = /module\.NewModule\(\s*([^,]+?)\s*,\s*module\.(Kind\w+)\s*\)/g;

function goSourcesIn(dir: string): string[] {
  const abs = join(SESAME, dir);
  // A hard failure, not a skip: a moved sesame tree must break this test rather
  // than let it pass over nothing.
  if (!existsSync(abs)) throw new Error(`sesame source not found at ${abs} — fix this test's path, do not delete it`);
  return readdirSync(abs)
    .filter((name) => name.endsWith('.go') && !name.endsWith('_test.go'))
    .map((name) => readFileSync(join(abs, name), 'utf8'));
}

const SOURCES = GO_DIRS.flatMap(goSourcesIn);

function matches(re: RegExp): RegExpMatchArray[] {
  return SOURCES.flatMap((src) => [...src.matchAll(re)]);
}

// Every `<ident>ModuleName = "<name>"` constant, keyed by identifier. These are
// the ModuleView keys, including the ones no module.NewModule call names —
// timers is written by the dashboard and read by the engine's Valkey clock, so
// it has a key without being a module.
function moduleNameConstants(): Map<string, string> {
  return new Map(matches(CONST_DECL).map((m) => [m[1], m[2]]));
}

// engine.LoyaltyModuleName -> LoyaltyModuleName -> "loyalty"; a quoted literal
// is already the name.
function resolveName(arg: string, consts: Map<string, string>): string | undefined {
  return arg.startsWith('"') ? arg.slice(1, -1) : consts.get(arg.replace(/^\w+\./, ''));
}

// The modules that own a ModuleView row. KindCore modules are the always-on
// built-ins: no row, never in the catalog.
function namedGoModules(consts: Map<string, string>): Set<string> {
  const names = matches(NEW_MODULE)
    .filter((m) => m[2] !== 'KindCore')
    .map((m) => resolveName(m[1], consts))
    .filter((name): name is string => !!name);
  return new Set(names);
}

describe('MOD against the sesame module registry', () => {
  const consts = moduleNameConstants();
  const named = namedGoModules(consts);
  const goKeys = new Set([...named, ...consts.values()]);

  // Guards the parser itself: a rewritten module package that no longer matches
  // these two shapes would otherwise make every assertion below trivially true.
  test('the Go source parses into a plausible registry', () => {
    expect(consts.size).toBeGreaterThan(10);
    expect(named.size).toBeGreaterThan(10);
    expect(goKeys.has('govee')).toBe(true);
    expect(goKeys.has('timers')).toBe(true);
  });

  test('every MOD id is a ModuleView key sesame actually reads', () => {
    const orphans = Object.values(MOD).filter((id) => !goKeys.has(id) && !(id in NO_GO_MODULE));
    expect(orphans).toEqual([]);
  });

  test('every named sesame module has a MOD id', () => {
    const modValues = new Set<string>(Object.values(MOD));
    const missing = [...named].filter((name) => !modValues.has(name) && !(name in NOT_IN_DASHBOARD)).sort();
    expect(missing).toEqual([]);
  });

  // The exemption lists are the documentation; an entry that stops being needed
  // has to be deleted, or it silently re-opens the hole it was carved for.
  test('no exemption outlives its reason', () => {
    const modValues = new Set<string>(Object.values(MOD));
    expect(Object.keys(NO_GO_MODULE).filter((id) => goKeys.has(id))).toEqual([]);
    expect(Object.keys(NOT_IN_DASHBOARD).filter((name) => modValues.has(name))).toEqual([]);
  });
});
