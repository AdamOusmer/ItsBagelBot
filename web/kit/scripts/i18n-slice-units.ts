// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { ROOT_LAYOUT, nodeIndex, type Chunk } from './i18n-slice-graph.ts';

/** The pseudo-unit for the client entry chunks, which can lazily import a unit before any node loads. */
export const ENTRY = '\0entry';

/**
 * A unit is a module that registers its own messages: a SvelteKit node module or a lazily imported app module.
 * The chunk holding a unit carries the English strings and slices for every module its chunk walk reaches
 * that its importers do not already guarantee.
 */
export interface UnitGraph {
  chunks: Record<string, Chunk>;
  moduleKeys: ReadonlyMap<string, ReadonlySet<string>>;
  ancestors: ReadonlyMap<number, ReadonlySet<number>>;
  units: ReadonlySet<string>;
}

interface Walk {
  keys: Set<string>;
  hits: Set<string>;
}

const union = (sets: Iterable<ReadonlySet<string>>): Set<string> => new Set([...sets].flatMap((set) => [...set]));

function intersect(sets: readonly ReadonlySet<string>[]): Set<string> {
  const [first, ...rest] = sets;
  return new Set([...(first ?? [])].filter((key) => rest.every((set) => set.has(key))));
}

export class SliceBuilder {
  private readonly unitChunks = new Map<string, Chunk>();
  private readonly chunkUnits = new Map<string, string>();
  private readonly walks = new Map<string, Walk>();
  private readonly covering = new Map<string, Set<string>>();
  private readonly pending = new Set<string>();

  constructor(private readonly graph: UnitGraph) {
    for (const chunk of Object.values(graph.chunks)) {
      for (const id of chunk.moduleIds.filter((module) => graph.units.has(module))) {
        this.unitChunks.set(id, chunk);
        this.chunkUnits.set(chunk.fileName, id);
      }
    }
  }

  unitsIn(chunk: { moduleIds: readonly string[] }): string[] {
    return chunk.moduleIds.filter((id) => this.graph.units.has(id));
  }

  private walk(start: Chunk): Walk {
    if (!this.walks.has(start.fileName)) this.walks.set(start.fileName, this.collect(start));
    return this.walks.get(start.fileName)!;
  }

  private collect(start: Chunk): Walk {
    const seen = new Set([start.fileName]);
    const hits = new Set<string>();
    const stack = [start];
    while (stack.length) {
      const at = stack.pop()!;
      for (const file of [...at.imports, ...at.dynamicImports].filter((next) => !seen.has(next) && this.graph.chunks[next])) {
        seen.add(file);
        const unit = this.chunkUnits.get(file);
        if (unit) hits.add(unit);
        else stack.push(this.graph.chunks[file]);
      }
    }
    const modules = [...seen].filter((file) => file === start.fileName || !this.chunkUnits.has(file)).flatMap((file) => this.graph.chunks[file].moduleIds);
    return { keys: union(modules.map((id) => this.graph.moduleKeys.get(id) ?? new Set())), hits };
  }

  private entryChunks(): Chunk[] {
    return Object.values(this.graph.chunks).filter((chunk) => chunk.isEntry && !this.chunkUnits.has(chunk.fileName));
  }

  private nodeUnit(index: number): string {
    const unit = [...this.unitChunks.keys()].find((id) => nodeIndex(id) === index);
    if (!unit) throw new Error(`i18n slices: no chunk for node ${index}`);
    return unit;
  }

  demand(unit: string): Set<string> {
    if (unit === ENTRY) return union(this.entryChunks().map((chunk) => this.walk(chunk).keys));
    const own = new Set(this.walk(this.unitChunks.get(unit)!).keys);
    if (nodeIndex(unit) === ROOT_LAYOUT) this.demand(ENTRY).forEach((key) => own.add(key));
    return own;
  }

  contexts(unit: string): string[] {
    const fromUnits = [...this.unitChunks.keys()].filter((other) => this.walk(this.unitChunks.get(other)!).hits.has(unit));
    const fromEntry = this.entryChunks().some((chunk) => this.walk(chunk).hits.has(unit));
    return fromEntry ? [...fromUnits, ENTRY] : fromUnits;
  }

  /** Keys guaranteed to be registered whenever `unit` renders, without counting the unit itself. */
  covered(unit: string): Set<string> {
    if (unit === ENTRY || this.pending.has(unit)) return new Set();
    if (!this.covering.has(unit)) {
      this.pending.add(unit);
      this.covering.set(unit, this.computeCovered(unit));
      this.pending.delete(unit);
    }
    return this.covering.get(unit)!;
  }

  private computeCovered(unit: string): Set<string> {
    const index = nodeIndex(unit);
    if (index !== undefined) return union([...(this.graph.ancestors.get(index) ?? [ROOT_LAYOUT])].map((ancestor) => this.demand(this.nodeUnit(ancestor))));
    return intersect(this.contexts(unit).map((context) => this.loadedBy(context)));
  }

  /** Keys certainly registered once `context` has imported something; the entry chunks register none themselves. */
  private loadedBy(context: string): Set<string> {
    return context === ENTRY ? new Set() : union([this.demand(context), this.covered(context)]);
  }

  ownKeys(unit: string): Set<string> {
    const covered = this.covered(unit);
    return new Set([...this.demand(unit)].filter((key) => !covered.has(key)));
  }
}
