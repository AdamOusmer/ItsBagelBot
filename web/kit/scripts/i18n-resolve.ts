// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Tables, type ImportedName, type TableResult } from './i18n-tables.ts';
import { Tree, isFunction, nameOf, propertyName, typeOf, type AstNode, type Path, type Ref } from './i18n-ast.ts';

export type Pattern = readonly string[];

export type TranslatorRole = 'translator' | 'raw';

export interface Deferred {
  kind: 'call' | 'table';
  source: string;
  imported: string;
  index: number;
  role: TranslatorRole;
  path: Path;
  site: string;
}

export interface Resolution {
  patterns: Pattern[];
  deferred: Deferred[];
}

const WILDCARD: Pattern = ['', ''];
const NOTHING: Resolution = { patterns: [], deferred: [] };
const EXPORT_DECLARATIONS = new Set(['ExportNamedDeclaration', 'ExportDefaultDeclaration']);

export const isBounded = (pattern: Pattern): boolean => pattern.join('') !== '';

export function patternRegExp(pattern: Pattern): RegExp {
  const escaped = pattern.map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
  return new RegExp(`^${escaped.join('[\\s\\S]*')}$`);
}

export const refAt = (ref: Ref, type: string, field: string): boolean => typeOf(ref.parent) === type && ref.field === field;

function joinParts(left: Pattern, right: Pattern): Pattern {
  return [...left.slice(0, -1), left[left.length - 1] + right[0], ...right.slice(1)];
}

function merge(a: Resolution | undefined, b: Resolution | undefined): Resolution | undefined {
  if (!a || !b) return undefined;
  return { patterns: [...a.patterns, ...b.patterns], deferred: [...a.deferred, ...b.deferred] };
}

const mergeAll = (parts: (Resolution | undefined)[]): Resolution | undefined => parts.reduce<Resolution | undefined>(merge, NOTHING);

function isSvelteCall(node: AstNode | undefined, method: string): boolean {
  const callee = typeOf(node) === 'CallExpression' ? (node!.callee as AstNode) : undefined;
  return typeOf(callee) === 'MemberExpression' && nameOf(callee!.object) === '$' && propertyName(callee!) === method;
}

const NOTHING_TESTS: Record<string, (node: AstNode, tree: Tree) => boolean> = {
  Literal: (node) => node.value === null,
  UnaryExpression: (node) => node.operator === 'void',
  Identifier: (node, tree) => node.name === 'undefined' && !tree.bindingOf(node)
};

const isNothing = (node: AstNode, tree: Tree): boolean => (NOTHING_TESTS[node.type] ?? (() => false))(node, tree);

const quasis = (node: AstNode): Resolution => ({ patterns: [(node.quasis as AstNode[]).map((quasi) => (quasi.value as { cooked: string }).cooked)], deferred: [] });

/** Resolves the set of keys an expression can evaluate to, or undefined when it cannot be bounded. */
export class KeyResolver {
  private readonly tables: Tables;
  private readonly resolving = new Set<AstNode>();
  private readonly byType: Record<string, (node: AstNode) => Resolution | undefined> = {
    Literal: (node) => (typeof node.value === 'string' ? { patterns: [[node.value]], deferred: [] } : undefined),
    TemplateLiteral: quasis,
    BinaryExpression: (node) => (node.operator === '+' ? this.concatenation(node) : undefined),
    ConditionalExpression: (node) => merge(this.resolve(node.consequent as AstNode), this.resolve(node.alternate as AstNode)),
    LogicalExpression: (node) => merge(this.resolve(node.left as AstNode), this.resolve(node.right as AstNode)),
    Identifier: (node) => this.constant(node) ?? this.fromTable(this.tables.value(node), node) ?? this.forwarded(node),
    CallExpression: (node) => this.derived(node),
    ChainExpression: (node) => this.resolve(node.expression as AstNode),
    MemberExpression: (node) => this.fromTable(this.tables.member(node), node)
  };

  constructor(
    private readonly tree: Tree,
    imports: ReadonlyMap<string, ImportedName>,
    private readonly snippet: (node: AstNode) => string
  ) {
    this.tables = new Tables(tree, imports);
  }

  resolve(node: AstNode): Resolution | undefined {
    if (this.resolving.has(node)) return undefined;
    this.resolving.add(node);
    try {
      return isNothing(node, this.tree) ? NOTHING : this.byType[node.type]?.(node);
    } finally {
      this.resolving.delete(node);
    }
  }

  private concatenation(node: AstNode): Resolution {
    const side = (part: AstNode) => {
      const patterns = this.resolve(part)?.patterns ?? [];
      return patterns.length ? patterns : [WILDCARD];
    };
    const patterns = side(node.left as AstNode).flatMap((left) => side(node.right as AstNode).map((right) => joinParts(left, right)));
    return { patterns, deferred: [] };
  }

  private constantInit(id: AstNode): AstNode | undefined {
    const binding = this.tree.constantBinding(id);
    const declarator = binding && this.tree.parents.get(binding);
    return typeOf(declarator) === 'VariableDeclarator' ? (declarator!.init as AstNode | undefined) : undefined;
  }

  private constant(id: AstNode): Resolution | undefined {
    const init = this.constantInit(id);
    return init && this.resolve(init);
  }

  private forwarded(id: AstNode): Resolution | undefined {
    const binding = this.tree.constantBinding(id);
    const fn = binding && this.tree.parents.get(binding);
    const index = isFunction(fn) ? (fn.params as AstNode[]).indexOf(binding!) : -1;
    const name = index < 0 ? undefined : this.functionName(fn!);
    const calls = name && this.directCalls(name);
    return calls ? mergeAll(calls.map((call) => this.argument(call, index))) : undefined;
  }

  private argument(call: AstNode, index: number): Resolution | undefined {
    const arg = (call.arguments as AstNode[])[index];
    return arg ? this.resolve(arg) : NOTHING;
  }

  private functionName(fn: AstNode): AstNode | undefined {
    if (fn.type === 'FunctionDeclaration') return fn.id as AstNode;
    const ref = this.tree.ref(fn);
    return refAt(ref, 'VariableDeclarator', 'init') && typeOf(ref.parent!.id) === 'Identifier' ? (ref.parent!.id as AstNode) : undefined;
  }

  /** Every call of a module-private function; undefined when it is exported or escapes, since then calls are unseen. */
  private directCalls(name: AstNode): AstNode[] | undefined {
    if (!this.tree.constantBinding(name) || this.isExported(name)) return undefined;
    const reads = this.tree.reads(name.name as string).filter((ref) => this.tree.bindingOf(ref.node) === name);
    return reads.every((ref) => refAt(ref, 'CallExpression', 'callee')) ? reads.map((ref) => ref.parent!) : undefined;
  }

  private isExported(name: AstNode): boolean {
    const holder = this.tree.parents.get(name)!;
    const statement = holder.type === 'VariableDeclarator' ? this.tree.parents.get(holder)! : holder;
    return EXPORT_DECLARATIONS.has(typeOf(this.tree.parents.get(statement)));
  }

  private derived(call: AstNode): Resolution | undefined {
    const body = this.derivedSource(call)?.body as AstNode | undefined;
    return body && body.type !== 'BlockStatement' ? this.resolve(body) : undefined;
  }

  private derivedSource(call: AstNode): AstNode | undefined {
    const arg = (call.arguments as AstNode[])[0];
    if (!isSvelteCall(call, 'get') || !this.readOnlyThroughGet(arg)) return undefined;
    const init = this.constantInit(arg);
    const source = isSvelteCall(init, 'derived') ? (init!.arguments as AstNode[])[0] : undefined;
    return typeOf(source) === 'ArrowFunctionExpression' ? source : undefined;
  }

  private readOnlyThroughGet(arg: AstNode | undefined): boolean {
    if (typeOf(arg) !== 'Identifier') return false;
    return this.tree.reads(arg!.name as string).every((ref) => refAt(ref, 'CallExpression', 'arguments') && isSvelteCall(ref.parent, 'get'));
  }

  private fromTable(result: TableResult, node: AstNode): Resolution | undefined {
    if (!result) return undefined;
    if ('strings' in result) return { patterns: result.strings.map((value) => [value]), deferred: [] };
    const deferred: Deferred = { kind: 'table', ...result.imported, index: 0, role: 'translator', path: result.path, site: this.snippet(node) };
    return { patterns: [], deferred: [deferred] };
  }
}
