// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { MessageTree } from '../lib/i18n/types.ts';
import { literalString, nameOf, typeOf, type AstNode } from './i18n-ast.ts';

export const ROOT_LAYOUT = 0;
export const ROOT_ERROR = 1;
const NODE_MODULE = /\/generated\/client-optimized\/nodes\/(\d+)\.js$/;

export interface Chunk {
  fileName: string;
  facadeModuleId: string | null;
  moduleIds: string[];
  imports: string[];
  dynamicImports: string[];
  isEntry: boolean;
}

export interface Route {
  id: string;
  nodes: number[];
  errors: number[];
}

export const isNodeModule = (id: string): boolean => NODE_MODULE.test(id);

export function nodeIndex(id: string | null | undefined): number | undefined {
  const match = NODE_MODULE.exec(id ?? '');
  return match ? Number(match[1]) : undefined;
}

const isBranch = (node: unknown): node is MessageTree => typeof node === 'object' && node !== null && !Array.isArray(node);

const join = (prefix: string, key: string): string => (prefix ? `${prefix}.${key}` : key);

export function leafPaths(tree: MessageTree, prefix = ''): string[] {
  return Object.entries(tree).flatMap(([key, value]) => (isBranch(value) ? leafPaths(value, join(prefix, key)) : [join(prefix, key)]));
}

function pickEntry(value: MessageTree[string], keys: ReadonlySet<string>, path: string): MessageTree[string] | undefined {
  if (!isBranch(value)) return keys.has(path) ? value : undefined;
  const branch = pick(value, keys, path);
  return Object.keys(branch).length ? branch : undefined;
}

export function pick(tree: MessageTree, keys: ReadonlySet<string>, prefix = ''): MessageTree {
  const entries = Object.entries(tree).map(([key, value]) => [key, pickEntry(value, keys, join(prefix, key))] as const);
  return Object.fromEntries(entries.filter((entry): entry is readonly [string, MessageTree[string]] => entry[1] !== undefined));
}

function numberOf(node: unknown): number | undefined {
  if (typeOf(node) === 'UnaryExpression') return numberOf((node as AstNode).argument);
  const value = typeOf(node) === 'Literal' ? (node as AstNode).value : undefined;
  return typeof value === 'number' ? value : undefined;
}

function numbersOf(node: unknown): number[] {
  const elements = typeOf(node) === 'ArrayExpression' ? ((node as AstNode).elements as unknown[]) : [];
  return elements.map(numberOf).filter((value): value is number => value !== undefined);
}

function declaratorsOf(statement: AstNode): AstNode[] {
  const declaration = statement.type === 'ExportNamedDeclaration' ? statement.declaration : statement;
  return typeOf(declaration) === 'VariableDeclaration' ? ((declaration as AstNode).declarations as AstNode[]) : [];
}

function routeOf(property: AstNode): Route {
  const [leaf, layouts, errors] = (property.value as AstNode).elements as AstNode[];
  return { id: literalString(property.key) ?? '', nodes: [...numbersOf(layouts), numberOf(leaf)!], errors: numbersOf(errors) };
}

/** Reads SvelteKit's generated client route table: each route's layout and leaf nodes plus its error nodes. */
export function parseDictionary(program: AstNode): Route[] {
  const declarator = (program.body as AstNode[]).flatMap(declaratorsOf).find((candidate) => nameOf(candidate.id) === 'dictionary');
  const properties = typeOf(declarator?.init) === 'ObjectExpression' ? ((declarator!.init as AstNode).properties as AstNode[]) : [];
  if (!properties.length) throw new Error('i18n slices: no routes in the client dictionary');
  return properties.map(routeOf);
}

/** For every node, the nodes SvelteKit always has loaded alongside it, on every route that uses it. */
export function alwaysLoadedWith(routes: readonly Route[]): Map<number, Set<number>> {
  const out = new Map<number, Set<number>>([
    [ROOT_LAYOUT, new Set()],
    [ROOT_ERROR, new Set([ROOT_LAYOUT])]
  ]);
  const meet = (node: number, loaded: number[]) => {
    const next = new Set([ROOT_LAYOUT, ...loaded].filter((other) => other !== node));
    const previous = out.get(node) ?? next;
    out.set(node, new Set([...previous].filter((other) => next.has(other))));
  };
  for (const route of routes) {
    route.nodes.forEach((node, index) => meet(node, route.nodes.slice(0, index)));
    route.errors.forEach((error) => meet(error, []));
  }
  return out;
}

/** Computes which message keys each node chunk must carry, from the keys of every module its chunks hold. */
export class SliceBuilder {
  private readonly closures = new Map<string, Set<string>>();

  constructor(
    private readonly chunks: Record<string, Chunk>,
    private readonly moduleKeys: ReadonlyMap<string, ReadonlySet<string>>,
    private readonly ancestors: ReadonlyMap<number, ReadonlySet<number>>
  ) {}

  private isNodeChunk(fileName: string): boolean {
    return nodeIndex(this.chunks[fileName]?.facadeModuleId) !== undefined;
  }

  private reachable(start: Chunk): Chunk[] {
    const seen = new Map<string, Chunk>([[start.fileName, start]]);
    const stack = [start];
    while (stack.length) {
      const at = stack.pop()!;
      const next = [...at.imports, ...at.dynamicImports].filter((file) => !seen.has(file) && !this.isNodeChunk(file) && this.chunks[file]);
      for (const file of next) {
        seen.set(file, this.chunks[file]);
        stack.push(this.chunks[file]);
      }
    }
    return [...seen.values()];
  }

  private closureKeys(chunk: Chunk): Set<string> {
    if (!this.closures.has(chunk.fileName)) {
      const modules = this.reachable(chunk).flatMap((at) => at.moduleIds);
      this.closures.set(chunk.fileName, new Set(modules.flatMap((id) => [...(this.moduleKeys.get(id) ?? [])])));
    }
    return this.closures.get(chunk.fileName)!;
  }

  private entryKeys(): string[] {
    const entries = Object.values(this.chunks).filter((chunk) => chunk.isEntry && nodeIndex(chunk.facadeModuleId) === undefined);
    return entries.flatMap((chunk) => [...this.closureKeys(chunk)]);
  }

  demand(node: number): Set<string> {
    const chunk = Object.values(this.chunks).find((candidate) => nodeIndex(candidate.facadeModuleId) === node);
    if (!chunk) throw new Error(`i18n slices: no chunk for node ${node}`);
    const own = new Set(this.closureKeys(chunk));
    if (node === ROOT_LAYOUT) this.entryKeys().forEach((key) => own.add(key));
    return own;
  }

  ownKeys(node: number): Set<string> {
    const ancestors = [...(this.ancestors.get(node) ?? [ROOT_LAYOUT])];
    const covered = new Set(ancestors.flatMap((ancestor) => [...this.demand(ancestor)]));
    return new Set([...this.demand(node)].filter((key) => !covered.has(key)));
  }
}
