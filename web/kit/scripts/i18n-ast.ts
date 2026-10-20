// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface AstNode {
  type: string;
  start: number;
  end: number;
  [field: string]: unknown;
}

export interface Ref {
  node: AstNode;
  parent: AstNode | undefined;
  field: string;
}

export type Path = readonly (string | undefined)[];

export const FUNCTION_TYPES = new Set(['FunctionDeclaration', 'FunctionExpression', 'ArrowFunctionExpression']);
const BLOCK_TYPES = new Set(['BlockStatement', 'StaticBlock', 'ForStatement', 'ForInStatement', 'ForOfStatement', 'SwitchStatement', ...FUNCTION_TYPES]);
const NON_REFERENCE_FIELDS: Record<string, string> = {
  MemberExpression: 'property',
  MethodDefinition: 'key',
  PropertyDefinition: 'key',
  Property: 'key',
  LabeledStatement: 'label',
  BreakStatement: 'label',
  ContinueStatement: 'label',
  MetaProperty: '*',
  ImportSpecifier: '*',
  ImportDefaultSpecifier: '*',
  ImportNamespaceSpecifier: '*',
  ExportSpecifier: 'exported',
  ExportAllDeclaration: 'exported'
};
const DECLARATION_FIELDS: Record<string, readonly string[]> = {
  VariableDeclarator: ['id'],
  CatchClause: ['param'],
  FunctionDeclaration: ['id', 'params'],
  FunctionExpression: ['id', 'params'],
  ArrowFunctionExpression: ['params'],
  ClassDeclaration: ['id'],
  ClassExpression: ['id']
};
const WRITE_FIELDS: Record<string, string> = {
  AssignmentExpression: 'left',
  ForInStatement: 'left',
  ForOfStatement: 'left',
  UpdateExpression: 'argument'
};
const PATTERN_FIELDS: Record<string, string> = { ArrayPattern: 'elements', RestElement: 'argument', AssignmentPattern: 'left' };

export const isNode = (value: unknown): value is AstNode =>
  typeof value === 'object' && value !== null && typeof (value as AstNode).type === 'string';

export const typeOf = (node: unknown): string => (isNode(node) ? node.type : '');

export const isFunction = (node: unknown): node is AstNode => FUNCTION_TYPES.has(typeOf(node));

export const nameOf = (node: unknown): string | undefined => (typeOf(node) === 'Identifier' ? ((node as AstNode).name as string) : undefined);

export const literalString = (node: unknown): string | undefined => {
  const value = typeOf(node) === 'Literal' ? (node as AstNode).value : undefined;
  return typeof value === 'string' ? value : undefined;
};

export const specifierName = (node: unknown): string | undefined => nameOf(node) ?? literalString(node);

export function propertyName(member: AstNode): string | undefined {
  return member.computed ? literalString(member.property) : nameOf(member.property);
}

export interface Span {
  start: number;
  end: number;
}

export const contains = (outer: Span, inner: Span): boolean => inner.start >= outer.start && inner.end <= outer.end;

export const allDefined = <T>(items: readonly (T | undefined)[]): items is T[] => items.every((item) => item !== undefined);

function childNodes(value: unknown): AstNode[] {
  if (Array.isArray(value)) return value.filter(isNode);
  return isNode(value) ? [value] : [];
}

function children(node: AstNode): [string, AstNode][] {
  return Object.entries(node).flatMap(([field, value]) => childNodes(value).map((child): [string, AstNode] => [field, child]));
}

function isNonReference({ parent, field }: Ref): boolean {
  const skip = NON_REFERENCE_FIELDS[typeOf(parent)];
  return skip === '*' || (skip === field && !parent!.computed);
}

function isDeclaration({ parent, field }: Ref): boolean {
  return (DECLARATION_FIELDS[typeOf(parent)] ?? []).includes(field);
}

export function isWrite({ parent, field }: Ref): boolean {
  if (typeOf(parent) === 'UnaryExpression') return parent!.operator === 'delete';
  return WRITE_FIELDS[typeOf(parent)] === field;
}

type ScopeRule = (tree: Tree, owner: AstNode) => AstNode | undefined;

const ownScope: ScopeRule = (_tree, owner) => owner;
const blockScope: ScopeRule = (tree, owner) => tree.climb(tree.parents.get(owner), (node) => BLOCK_TYPES.has(node.type));
const declaratorScope: ScopeRule = (tree, declarator) => {
  const declaration = tree.parents.get(declarator)!;
  const stop = declaration.kind === 'var' ? isFunction : (node: AstNode) => BLOCK_TYPES.has(node.type);
  return tree.climb(tree.parents.get(declaration), stop);
};

const SCOPE_RULES: Record<string, ScopeRule> = {
  'ArrowFunctionExpression.params': ownScope,
  'FunctionExpression.params': ownScope,
  'FunctionDeclaration.params': ownScope,
  'FunctionExpression.id': ownScope,
  'CatchClause.param': ownScope,
  'VariableDeclarator.id': declaratorScope,
  'FunctionDeclaration.id': blockScope,
  'ClassDeclaration.id': blockScope
};

const PATTERN_OWNERS = new Set(['ArrowFunctionExpression.params', 'FunctionExpression.params', 'FunctionDeclaration.params', 'CatchClause.param', 'VariableDeclarator.id']);

export class Tree {
  readonly parents = new Map<AstNode, AstNode>();
  readonly fields = new Map<AstNode, string>();
  readonly identifiers: AstNode[] = [];
  readonly nodes: AstNode[] = [];
  private readonly declared = new Map<string, AstNode[]>();
  private readonly writes: AstNode[] = [];

  constructor(readonly root: AstNode) {
    const stack: AstNode[] = [root];
    while (stack.length) this.visit(stack.pop()!, stack);
    for (const id of this.identifiers) this.recordBinding(id);
  }

  private visit(node: AstNode, stack: AstNode[]): void {
    this.nodes.push(node);
    if (node.type === 'Identifier') this.identifiers.push(node);
    for (const [field, child] of children(node).reverse()) {
      this.parents.set(child, node);
      this.fields.set(child, field);
      stack.push(child);
    }
  }

  ref(node: AstNode): Ref {
    return { node, parent: this.parents.get(node), field: this.fields.get(node) ?? '' };
  }

  climb(from: AstNode | undefined, stop: (node: AstNode) => boolean): AstNode | undefined {
    const halt = (node: AstNode) => node.type === 'Program' || stop(node);
    let at = from;
    while (at && !halt(at)) at = this.parents.get(at);
    return at;
  }

  insideFunction(node: AstNode): boolean {
    return isFunction(this.climb(this.parents.get(node), isFunction));
  }

  isRead(ref: Ref): boolean {
    const tests = [isNonReference, isDeclaration, isWrite, (r: Ref) => this.inPattern(r)];
    return !tests.some((test) => test(ref));
  }

  reads(name: string): Ref[] {
    return this.identifiers.filter((id) => id.name === name).map((id) => this.ref(id)).filter((ref) => this.isRead(ref));
  }

  declarations(name: string): AstNode[] {
    return this.declared.get(name) ?? [];
  }

  isWritten(name: string): boolean {
    return this.writes.some((write) => write.name === name);
  }

  private recordBinding(id: AstNode): void {
    const ref = this.ref(id);
    const pattern = this.inPattern(ref);
    if (this.declares(ref, pattern)) this.declared.set(id.name as string, [...this.declarations(id.name as string), id]);
    else if (pattern || isWrite(ref)) this.writes.push(id);
  }

  private declares(ref: Ref, pattern: boolean): boolean {
    return isDeclaration(ref) || (pattern && this.declaredPattern(ref.node));
  }

  private inPattern({ parent, field }: Ref): boolean {
    if (PATTERN_FIELDS[typeOf(parent)] === field) return true;
    return typeOf(parent) === 'Property' && field === 'value' && typeOf(this.parents.get(parent!)) === 'ObjectPattern';
  }

  private declaredPattern(node: AstNode): boolean {
    const owner = this.climb(node, (at) => isDeclaration(this.ref(at)) || isWrite(this.ref(at)));
    return owner !== undefined && isDeclaration(this.ref(owner));
  }

  scopeOfBinding(binding: AstNode): AstNode | undefined {
    const parent = this.parents.get(binding);
    const rule = SCOPE_RULES[`${typeOf(parent)}.${this.fields.get(binding)}`];
    return rule ? rule(this, parent!) : this.patternScope(binding);
  }

  private ownerKey(at: AstNode): string {
    return `${typeOf(this.parents.get(at))}.${this.fields.get(at)}`;
  }

  private patternScope(binding: AstNode): AstNode | undefined {
    const top = this.climb(binding, (at) => PATTERN_OWNERS.has(this.ownerKey(at)));
    const owner = top && PATTERN_OWNERS.has(this.ownerKey(top)) ? this.parents.get(top) : undefined;
    return owner && SCOPE_RULES[this.ownerKey(top!)](this, owner);
  }

  /** The declaring identifier a reference resolves to, when exactly one declaration owns the innermost scope. */
  bindingOf(id: AstNode): AstNode | undefined {
    const visible = this.declarations(id.name as string)
      .map((binding) => ({ binding, scope: this.scopeOfBinding(binding) }))
      .filter((entry): entry is { binding: AstNode; scope: AstNode } => entry.scope !== undefined && contains(entry.scope, id));
    const size = (entry: { scope: AstNode }) => entry.scope.end - entry.scope.start;
    const innermost = Math.min(...visible.map(size));
    const owners = visible.filter((entry) => size(entry) === innermost);
    return owners.length === 1 ? owners[0].binding : undefined;
  }

  /** Like bindingOf, but only for bindings never assigned after declaration. */
  constantBinding(id: AstNode): AstNode | undefined {
    const binding = this.bindingOf(id);
    const scope = binding && this.scopeOfBinding(binding);
    if (!scope) return undefined;
    return this.writes.some((write) => write.name === id.name && contains(scope, write)) ? undefined : binding;
  }
}

function isFreeze(callee: AstNode): boolean {
  return callee.type === 'MemberExpression' && nameOf(callee.object) === 'Object' && propertyName(callee) === 'freeze';
}

function frozenArgument(node: AstNode): AstNode | undefined {
  const freeze = node.type === 'CallExpression' && isFreeze(node.callee as AstNode);
  return freeze ? (node.arguments as AstNode[])[0] : undefined;
}

export interface TableRules {
  frozen: boolean;
  spread: (id: AstNode) => AstNode | undefined;
}

function unwrap(node: AstNode, rules: TableRules): AstNode | undefined {
  const inner = frozenArgument(node);
  if (inner) return inner;
  return rules.frozen ? undefined : node;
}

function spreadEntries(property: AstNode, name: string | undefined, rules: TableRules): AstNode[] | undefined {
  const argument = property.argument as AstNode;
  const table = argument.type === 'Identifier' ? rules.spread(argument) : undefined;
  return table && tableEntry(table, name, { ...rules, frozen: true });
}

const isPlainProperty = (property: AstNode): boolean => property.type === 'Property' && property.kind === 'init' && !property.computed;

function propertyEntries(property: AstNode, name: string | undefined, rules: TableRules): AstNode[] | undefined {
  if (property.type === 'SpreadElement') return spreadEntries(property, name, rules);
  if (!isPlainProperty(property)) return undefined;
  return name === undefined || specifierName(property.key) === name ? [property.value as AstNode] : [];
}

function objectEntries(node: AstNode, name: string | undefined, rules: TableRules): AstNode[] | undefined {
  const parts = (node.properties as AstNode[]).map((property) => propertyEntries(property, name, rules));
  return allDefined(parts) ? parts.flat() : undefined;
}

function arrayEntries(node: AstNode, name: string | undefined): AstNode[] | undefined {
  const elements = node.elements as (AstNode | null)[];
  if (!elements.every(isNode)) return undefined;
  if (name === undefined) return elements;
  return /^\d+$/.test(name) ? elements.slice(Number(name), Number(name) + 1) : undefined;
}

function tableEntry(node: AstNode, name: string | undefined, rules: TableRules): AstNode[] | undefined {
  const table = unwrap(node, rules);
  if (typeOf(table) === 'ObjectExpression') return objectEntries(table!, name, rules);
  return typeOf(table) === 'ArrayExpression' ? arrayEntries(table!, name) : undefined;
}

export function leafNodes(node: AstNode, path: Path, rules: TableRules): AstNode[] | undefined {
  if (!path.length) return [node];
  const next = tableEntry(node, path[0], rules);
  const leaves = (next ?? []).map((entry) => leafNodes(entry, path.slice(1), rules));
  return next && allDefined(leaves) ? leaves.flat() : undefined;
}

export function leafStrings(node: AstNode, path: Path, rules: TableRules): string[] | undefined {
  const leaves = leafNodes(node, path, rules);
  const values = (leaves ?? []).map(literalString);
  return leaves && allDefined(values) ? values : undefined;
}

export function primitiveLeaves(node: AstNode, path: Path, rules: TableRules): boolean {
  const leaves = leafNodes(node, path, rules);
  return leaves !== undefined && leaves.every((leaf) => leaf.type === 'Literal');
}

export function memberPath(member: AstNode): { root: AstNode; path: (string | undefined)[] } {
  const path: (string | undefined)[] = [];
  let at = member;
  while (at.type === 'MemberExpression') {
    path.unshift(propertyName(at));
    at = at.object as AstNode;
  }
  return { root: at, path };
}
