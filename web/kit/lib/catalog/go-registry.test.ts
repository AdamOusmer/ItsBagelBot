// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { MOD } from './index';

const SESAME = join(import.meta.dir, '../../../../app/twitch/sesame');
const GO_DIRS = ['modules', 'engine'];

const NO_GO_MODULE: Record<string, string> = {
  counters:
    'a catalog tool, not a module: counters are rows on the loyalty service, and the tile carries toggleable:false so no ModuleView row is ever written',
  stream:
    'KindCore stream-editor commands live on Cmd(); the tile is discovery plus the commands grant, toggleable:false so no ModuleView row named stream is ever written',
  discord:
    'companion module served directly by outgress (live/clips); outgress reads the ModuleView row on stream events without hopping through sesame'
};

const NOT_IN_DASHBOARD: Record<string, string> = {
  moderation: 'the mod-facing command set, configured through automod rather than its own tile'
};

const CONST_DECL = /(\w*[Mm]oduleName)\s*=\s*"([a-z]+)"/g;
const NEW_MODULE = /module\.NewModule\(\s*([^,]+?)\s*,\s*module\.(Kind\w+)\s*\)/g;

function goSourcesIn(dir: string): string[] {
  const abs = join(SESAME, dir);
  if (!existsSync(abs)) throw new Error(`sesame source not found at ${abs}. Fix this test's path, do not delete it`);
  return readdirSync(abs)
    .filter((name) => name.endsWith('.go') && !name.endsWith('_test.go'))
    .map((name) => readFileSync(join(abs, name), 'utf8'));
}

const SOURCES = GO_DIRS.flatMap(goSourcesIn);

function matches(re: RegExp): RegExpMatchArray[] {
  return SOURCES.flatMap((src) => [...src.matchAll(re)]);
}

function moduleNameConstants(): Map<string, string> {
  return new Map(matches(CONST_DECL).map((m) => [m[1], m[2]]));
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
});
