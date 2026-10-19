// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { MessageTree } from '../lib/i18n/types.ts';
import { ROOT_ERROR, ROOT_LAYOUT, leafPaths, nodeIndex, type Route } from './i18n-slice-graph.ts';
import { ENTRY } from './i18n-slice-units.ts';

export interface UnitSlice {
  demand: Set<string>;
  bodies: Record<string, string>;
  contexts: string[];
}

export interface SliceSet {
  slices: ReadonlyMap<string, UnitSlice>;
  locales: readonly string[];
  ancestors: ReadonlyMap<number, ReadonlySet<number>>;
}

class Coverage {
  private readonly nodes = new Map<number, string>();

  constructor(
    private readonly set: SliceSet,
    private readonly locale: string
  ) {
    for (const unit of set.slices.keys()) if (nodeIndex(unit) !== undefined) this.nodes.set(nodeIndex(unit)!, unit);
  }

  node(index: number, label: string): string {
    const unit = this.nodes.get(index);
    if (!unit) throw new Error(`i18n slices: ${label} uses node ${index}, which has no slice`);
    return unit;
  }

  delivered(unit: string): Set<string> {
    return new Set(leafPaths(JSON.parse(this.set.slices.get(unit)!.bodies[this.locale])));
  }

  private withNode(index: number): Set<string> {
    const loaded = [index, ...(this.set.ancestors.get(index) ?? [ROOT_LAYOUT])];
    return new Set(loaded.flatMap((node) => [...this.delivered(this.node(node, `node ${index}`))]));
  }

  private context(unit: string, seen: ReadonlySet<string>): Set<string> {
    if (unit === ENTRY) return this.delivered(this.node(ROOT_LAYOUT, 'entry'));
    const index = nodeIndex(unit);
    return index === undefined ? this.guaranteed(unit, seen) : this.withNode(index);
  }

  /** Keys certainly registered when a lazy unit renders: its own slice plus what every importer path guarantees. */
  guaranteed(unit: string, seen: ReadonlySet<string> = new Set()): Set<string> {
    const own = this.delivered(unit);
    const contexts = this.set.slices.get(unit)!.contexts.filter((context) => !seen.has(context));
    if (!contexts.length) return own;
    const shared = contexts.map((context) => this.context(context, new Set([...seen, unit])));
    const [first, ...rest] = shared;
    return new Set([...own, ...[...first].filter((key) => rest.every((set) => set.has(key)))]);
  }
}

function assertKeys(demand: Iterable<string>, present: ReadonlySet<string>, label: string): void {
  const missing = [...demand].filter((key) => !present.has(key));
  if (missing.length) throw new Error(`i18n slices: ${label} lacks ${missing.length} key(s), first ${missing[0]}`);
}

function assertNodes(coverage: Coverage, set: SliceSet, nodes: readonly number[], label: string): void {
  const units = nodes.map((node) => coverage.node(node, label));
  const present = new Set(units.flatMap((unit) => [...coverage.delivered(unit)]));
  assertKeys(units.flatMap((unit) => [...set.slices.get(unit)!.demand]), present, label);
}

function assertLocale(set: SliceSet, routes: readonly Route[], locale: string): void {
  const coverage = new Coverage(set, locale);
  for (const route of routes) {
    assertNodes(coverage, set, [ROOT_LAYOUT, ...route.nodes], `${route.id} (${locale})`);
    route.errors.forEach((error) => assertNodes(coverage, set, [ROOT_LAYOUT, error], `${route.id} error ${error} (${locale})`));
  }
  assertNodes(coverage, set, [ROOT_LAYOUT, ROOT_ERROR], `root error (${locale})`);
  const lazy = [...set.slices.keys()].filter((unit) => nodeIndex(unit) === undefined);
  lazy.forEach((unit) => assertKeys(set.slices.get(unit)!.demand, coverage.guaranteed(unit), `${unit} (${locale})`));
}

/**
 * Fails the build unless every route, error page and lazily imported module gets, in every locale, every key the
 * modules in its chunks can translate, from its own slice or slices that are certainly loaded alongside it.
 */
export function assertCoverage(set: SliceSet, routes: readonly Route[], english: MessageTree): void {
  const known = new Set(leafPaths(english));
  const unknown = [...set.slices.values()].flatMap((slice) => [...slice.demand]).find((key) => !known.has(key));
  if (unknown) throw new Error(`i18n slices: ${unknown} is not an English key`);
  set.locales.forEach((locale) => assertLocale(set, routes, locale));
}
