// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { MessageTree } from '../lib/i18n/types.ts';
import { ROOT_ERROR, ROOT_LAYOUT, leafPaths, type Route } from './i18n-slice-graph.ts';

export interface NodeSlice {
  demand: Set<string>;
  bodies: Record<string, string>;
}

export interface SliceSet {
  slices: ReadonlyMap<number, NodeSlice>;
  locales: readonly string[];
}

function delivered(set: SliceSet, nodes: readonly number[], locale: string): Set<string> {
  return new Set(nodes.flatMap((node) => leafPaths(JSON.parse(set.slices.get(node)!.bodies[locale]))));
}

function missingNode(set: SliceSet, nodes: readonly number[]): number | undefined {
  return nodes.find((node) => !set.slices.has(node));
}

function assertNodes(set: SliceSet, nodes: readonly number[], label: string): void {
  const absent = missingNode(set, nodes);
  if (absent !== undefined) throw new Error(`i18n slices: ${label} uses node ${absent}, which has no slice`);
  const demand = new Set(nodes.flatMap((node) => [...set.slices.get(node)!.demand]));
  for (const locale of set.locales) {
    const present = delivered(set, nodes, locale);
    const missing = [...demand].filter((key) => !present.has(key));
    if (missing.length) throw new Error(`i18n slices: ${label} lacks ${missing.length} ${locale} key(s), first ${missing[0]}`);
  }
}

/**
 * Fails the build unless, for every route and every error page, the slices SvelteKit loads together deliver every
 * key the modules in their chunks can translate, in every locale.
 */
export function assertCoverage(set: SliceSet, routes: readonly Route[], english: MessageTree): void {
  const known = new Set(leafPaths(english));
  const unknown = [...set.slices.values()].flatMap((slice) => [...slice.demand]).find((key) => !known.has(key));
  if (unknown) throw new Error(`i18n slices: ${unknown} is not an English key`);
  for (const route of routes) {
    assertNodes(set, [ROOT_LAYOUT, ...route.nodes], route.id);
    route.errors.forEach((error) => assertNodes(set, [ROOT_LAYOUT, error], `${route.id} error ${error}`));
  }
  assertNodes(set, [ROOT_LAYOUT, ROOT_ERROR], 'root error');
}
