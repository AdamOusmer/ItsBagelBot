// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { MOD, MODULE_CATALOG, moduleAllowedTriggers, moduleDef } from './index';

const SESAME = join(import.meta.dir, '../../../../app/twitch/sesame');

const NO_GO_MODULE: Record<string, string> = {
  counters:
    'a catalog tool, not a module: counters are rows on the loyalty service, and the tile carries toggleable:false so no ModuleView row is ever written',
  stream:
    'KindCore stream-editor commands live on StreamEditors(); the tile is discovery plus the commands grant, toggleable:false so no ModuleView row named stream is ever written',
};

const NOT_IN_DASHBOARD: Record<string, string> = {
  moderation: 'the mod-facing command set, configured through automod rather than its own tile'
};

const CONST_DECL = /(\w*[Mm]oduleName)\s*=\s*"([a-z]+)"/g;
const CONST_ALIAS = /(\w*[Mm]oduleName)\s*=\s*\w+\.(\w*[Mm]oduleName)/g;
const NEW_MODULE = /module\.NewModule\(\s*([^,]+?)\s*,\s*module\.(Kind\w+)\s*\)/g;
const CMD_DECL = /\bm\.Command\(\s*([^)]+?)\s*\)/g;
const ALIAS_DECL = /\.\s*Aliases\(([^)]+)\)/g;
const QUOTED = /"([a-z0-9]+)"/g;

const DRAW_RAFFLE_EXCLUDE = new Set(['check', 'claim']);

function goSourcesIn(dir: string): string[] {
  const abs = join(SESAME, dir);
  if (!existsSync(abs)) throw new Error(`sesame source not found at ${abs}. Fix this test's path, do not delete it`);
  return readdirSync(abs)
    .filter((name) => name.endsWith('.go') && !name.endsWith('_test.go'))
    .map((name) => readFileSync(join(abs, name), 'utf8'));
}

const MODULE_SOURCES = goSourcesIn('modules');
const SOURCES = [...MODULE_SOURCES, ...goSourcesIn('engine')];

function matches(re: RegExp): RegExpMatchArray[] {
  return SOURCES.flatMap((src) => [...src.matchAll(re)]);
}

function moduleNameConstants(): Map<string, string> {
  const map = new Map(matches(CONST_DECL).map((m) => [m[1], m[2]]));
  for (const m of matches(CONST_ALIAS)) {
    const target = map.get(m[2]);
    if (target) map.set(m[1], target);
  }
  return map;
}

function resolveName(arg: string, consts: Map<string, string>): string | undefined {
  return arg.startsWith('"') ? arg.slice(1, -1) : consts.get(arg.replace(/^\w+\./, ''));
}

function namedGoModules(consts: Map<string, string>): Set<string> {
  const names = matches(NEW_MODULE)
    .filter((m) => m[2] !== 'KindCore')
    .map((m) => resolveName(m[1], consts))
    .filter((name): name is string => !!name);
  return new Set(names);
}

function addQuoted(match: RegExpMatchArray, out: Set<string>): void {
  for (const q of match[1].matchAll(QUOTED)) out.add(q[1]);
}

function parseBlockTriggers(block: string, consts: Map<string, string>): Set<string> {
  const triggers = new Set<string>();
  for (const m of block.matchAll(CMD_DECL)) {
    const name = resolveName(m[1], consts);
    if (name) triggers.add(name);
  }
  for (const m of block.matchAll(ALIAS_DECL)) addQuoted(m, triggers);
  return triggers;
}

function collectSourceTriggers(src: string, consts: Map<string, string>, out: Map<string, Set<string>>): void {
  for (const m of src.matchAll(NEW_MODULE)) {
    const name = resolveName(m[1], consts);
    if (!name) continue;
    const start = m.index ?? 0;
    const end = src.indexOf('.Build()', start);
    const block = end >= 0 ? src.slice(start, end) : src.slice(start);
    const triggers = parseBlockTriggers(block, consts);
    const bucket = out.get(name) ?? new Set<string>();
    for (const t of triggers) bucket.add(t);
    out.set(name, bucket);
  }
}

function goModuleTriggerMap(consts: Map<string, string>): Map<string, Set<string>> {
  const out = new Map<string, Set<string>>();
  for (const src of MODULE_SOURCES) collectSourceTriggers(src, consts, out);
  const draw = out.get('draw') ?? new Set<string>();
  for (const trigger of draw) {
    out.get('giveaway')?.add(trigger);
    if (!DRAW_RAFFLE_EXCLUDE.has(trigger)) out.get('raffle')?.add(trigger);
  }
  return out;
}

describe('MOD against the sesame module registry', () => {
  const consts = moduleNameConstants();
  const named = namedGoModules(consts);
  const goKeys = new Set([...named, ...consts.values()]);

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

  test('no exemption outlives its reason', () => {
    const modValues = new Set<string>(Object.values(MOD));
    expect(Object.keys(NO_GO_MODULE).filter((id) => goKeys.has(id))).toEqual([]);
    expect(Object.keys(NOT_IN_DASHBOARD).filter((name) => modValues.has(name))).toEqual([]);
  });

  test('toggleable catalog triggers match every registered Go command and alias', () => {
    const goTriggers = goModuleTriggerMap(consts);
    const mismatches: Record<string, { go: string[]; catalog: string[] }> = {};
    const ids = new Set<string>([
      ...MODULE_CATALOG.filter((def) => def.toggleable !== false).map((def) => def.id),
      ...[...named].filter((name) => !(name in NOT_IN_DASHBOARD))
    ]);
    for (const id of [...ids].sort()) {
      const go = [...(goTriggers.get(id) ?? [])].sort();
      const catalog = [...moduleAllowedTriggers(moduleDef(id)!)].sort();
      if (go.join(',') !== catalog.join(',')) mismatches[id] = { go, catalog };
    }
    expect(mismatches).toEqual({});
  });
});
