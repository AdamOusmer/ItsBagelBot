// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AstNode } from './i18n-ast.ts';
import {
  I18N_API,
  analyzeExportedFunction,
  exportTarget,
  exportedTableStrings,
  isBounded,
  patternRegExp,
  type Deferred,
  type Pattern,
  type Usage
} from './i18n-usage.ts';

export interface LinkHost {
  resolve: (source: string, importer: string) => Promise<string | undefined>;
  code: (id: string) => string | undefined;
  exports: (id: string) => readonly string[];
  parse: (code: string) => AstNode;
}

export interface Linked {
  patterns: Pattern[];
  problems: string[];
}

interface Location {
  id: string;
  local: string;
}

interface Lookup {
  importer: string;
  source: string;
  name: string;
  hops: number;
}

const MAX_HOPS = 16;

const failed = (problem: string): Linked => ({ patterns: [], problems: [problem] });

/** Follows deferred translator hand-offs and key tables across modules to the code that defines them. */
export class Linker {
  private readonly programs = new Map<string, AstNode>();
  private readonly calls = new Map<string, Promise<Linked>>();

  constructor(private readonly host: LinkHost) {}

  async link(id: string, usage: Usage): Promise<Linked> {
    const out: Linked = { patterns: [...usage.patterns], problems: [...usage.problems] };
    for (const item of usage.deferred) this.absorb(out, await this.deferred(id, item));
    for (const source of usage.namespaces) await this.checkNamespace(out, { importer: id, source, name: '*', hops: 0 });
    return out;
  }

  private absorb(into: Linked, from: Linked): void {
    into.patterns.push(...from.patterns);
    into.problems.push(...from.problems);
  }

  private program(id: string): AstNode | undefined {
    if (!this.programs.has(id)) {
      const code = this.host.code(id);
      if (code !== undefined) this.programs.set(id, this.host.parse(code));
    }
    return this.programs.get(id);
  }

  private async locate(lookup: Lookup): Promise<Location | undefined> {
    const id = lookup.hops < MAX_HOPS ? await this.host.resolve(lookup.source, lookup.importer) : undefined;
    const program = id && this.program(id);
    return program ? this.locateIn(id!, program, lookup) : undefined;
  }

  private locateIn(id: string, program: AstNode, lookup: Lookup): Promise<Location | undefined> | Location {
    const target = exportTarget(program, lookup.name);
    const hops = lookup.hops + 1;
    if (target.source && target.imported) return this.locate({ importer: id, source: target.source, name: target.imported, hops });
    if (target.local) return { id, local: target.local };
    return this.locateStar(id, target.stars, { ...lookup, hops });
  }

  private async locateStar(id: string, stars: string[], lookup: Lookup): Promise<Location | undefined> {
    for (const star of stars) {
      const starId = await this.host.resolve(star, id);
      if (starId && this.host.exports(starId).includes(lookup.name)) return this.locate({ ...lookup, importer: id, source: star });
    }
    return undefined;
  }

  private async deferred(importer: string, item: Deferred): Promise<Linked> {
    const location = await this.locate({ importer, source: item.source, name: item.imported, hops: 0 });
    if (!location) return failed(`unresolved import ${item.imported} from ${item.source}: ${item.site}`);
    return item.kind === 'table' ? this.table(location, item) : this.call(location, item);
  }

  private table(location: Location, item: Deferred): Linked {
    const strings = exportedTableStrings(this.program(location.id)!, location.local, item.path) ?? [];
    if (!strings.length) return failed(`imported key table is not a frozen literal of strings: ${item.site}`);
    return { patterns: strings.map((value) => [value]), problems: [] };
  }

  private call(location: Location, item: Deferred): Promise<Linked> {
    const key = `${location.id}#${location.local}#${item.index}#${item.role}`;
    if (!this.calls.has(key)) this.calls.set(key, this.analyzeCall(location, item));
    return this.calls.get(key)!;
  }

  private async analyzeCall(location: Location, item: Deferred): Promise<Linked> {
    const usage = analyzeExportedFunction(this.program(location.id)!, this.host.code(location.id)!, location.local, item);
    const linked = await this.link(location.id, usage);
    return { patterns: linked.patterns, problems: linked.problems.map((problem) => `${problem} (via ${item.site})`) };
  }

  private async checkNamespace(out: Linked, lookup: Lookup): Promise<void> {
    const id = await this.host.resolve(lookup.source, lookup.importer);
    const exported = id ? this.host.exports(id) : [...I18N_API];
    if (exported.some((name) => I18N_API.has(name))) out.problems.push(`namespace import of ${lookup.source} escapes with i18n exports`);
  }
}

/** Expands key patterns over the English catalog's keys. */
export class KeyCatalog {
  private readonly known: ReadonlySet<string>;

  constructor(private readonly keys: readonly string[]) {
    this.known = new Set(keys);
  }

  match(patterns: readonly Pattern[]): { keys: Set<string>; problems: string[] } {
    const found = new Set<string>();
    const problems: string[] = [];
    for (const pattern of patterns) {
      const { keys, problem } = this.matchOne(pattern);
      keys.forEach((key) => found.add(key));
      if (problem) problems.push(problem);
    }
    return { keys: found, problems };
  }

  private matchOne(pattern: Pattern): { keys: readonly string[]; problem?: string } {
    if (!isBounded(pattern)) return { keys: [], problem: 'unbounded key pattern' };
    if (pattern.length === 1) return this.known.has(pattern[0]) ? { keys: pattern } : { keys: [], problem: `unknown key ${pattern[0]}` };
    const matcher = patternRegExp(pattern);
    const keys = this.keys.filter((key) => matcher.test(key));
    return keys.length ? { keys } : { keys, problem: `no key matches ${pattern.join('*')}` };
  }
}
