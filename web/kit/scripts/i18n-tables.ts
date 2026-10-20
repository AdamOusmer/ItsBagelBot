// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  Tree,
  isFunction,
  isWrite,
  leafStrings,
  memberPath,
  nameOf,
  primitiveLeaves,
  propertyName,
  specifierName,
  typeOf,
  type AstNode,
  type Path,
  type Ref,
  type TableRules
} from './i18n-ast.ts';

export interface ImportedName {
  source: string;
  imported: string;
}

export type TableResult = { strings: string[] } | { imported: ImportedName; path: Path } | undefined;

interface Lookup {
  root: AstNode;
  path: Path;
  aliases: number;
}

const ITERATION = new Set(['map', 'flatMap', 'forEach', 'filter', 'find', 'findLast', 'some', 'every']);
const MAX_ALIASES = 8;
const ALIAS_TYPES = new Set(['Identifier', 'MemberExpression']);

const found = (strings: string[] | undefined): TableResult => strings && { strings };

function outermostMember(tree: Tree, member: AstNode): AstNode {
  let outer = member;
  while (typeOf(tree.parents.get(outer)) === 'MemberExpression' && tree.fields.get(outer) === 'object') outer = tree.parents.get(outer)!;
  return outer;
}

const isIterationMember = (member: AstNode): boolean => !member.computed && ITERATION.has(propertyName(member) ?? '');

/** The `X.map(fn)`-style call that hands `fn` the items of X, if `fn` is such a callback. */
function iterationCall(tree: Tree, fn: AstNode): AstNode | undefined {
  const call = tree.parents.get(fn);
  if (typeOf(call) !== 'CallExpression' || (call!.arguments as AstNode[])[0] !== fn) return undefined;
  const callee = call!.callee as AstNode;
  return typeOf(callee) === 'MemberExpression' && isIterationMember(callee) ? call : undefined;
}

/** Resolves key lookups in constant tables whose string leaves cannot change at run time. */
export class Tables {
  constructor(
    private readonly tree: Tree,
    private readonly imports: ReadonlyMap<string, ImportedName>
  ) {}

  member(member: AstNode): TableResult {
    const { root, path } = memberPath(member);
    return root.type === 'Identifier' ? this.at({ root, path, aliases: 0 }) : undefined;
  }

  value(id: AstNode): TableResult {
    return this.at({ root: id, path: [], aliases: 0 });
  }

  private rules(frozen: boolean): TableRules {
    return { frozen, spread: (id) => this.initOf(this.tree.constantBinding(id)) };
  }

  private initOf(binding: AstNode | undefined): AstNode | undefined {
    const declarator = binding && this.tree.parents.get(binding);
    return typeOf(declarator) === 'VariableDeclarator' && declarator!.id === binding ? (declarator!.init as AstNode | undefined) : undefined;
  }

  private imported({ root, path }: Lookup): TableResult {
    const imported = this.imports.get(root.name as string);
    return imported && !this.tree.bindingOf(root) ? { imported, path } : undefined;
  }

  private at(lookup: Lookup): TableResult {
    const binding = this.tree.constantBinding(lookup.root);
    if (!binding) return this.imported(lookup);
    const source = this.elementSource(binding);
    if (source) return this.elements(source, binding, lookup);
    const init = this.initOf(binding);
    if (!init || lookup.aliases > MAX_ALIASES) return undefined;
    return ALIAS_TYPES.has(init.type) ? this.alias(init, lookup) : this.literal(binding, init, lookup.path);
  }

  private alias(init: AstNode, lookup: Lookup): TableResult {
    const { root, path } = memberPath(init);
    return root.type === 'Identifier' ? this.at({ root, path: [...path, ...lookup.path], aliases: lookup.aliases + 1 }) : undefined;
  }

  private literal(binding: AstNode, init: AstNode, path: Path): TableResult {
    const frozen = leafStrings(init, path, this.rules(true));
    if (frozen) return { strings: frozen };
    const trusted = !this.exported(binding) && this.frozenLocally(binding, init);
    return trusted ? found(leafStrings(init, path, this.rules(false))) : undefined;
  }

  private elements(source: AstNode, param: AstNode, lookup: Lookup): TableResult {
    const path = [undefined, ...lookup.path];
    if (source.type === 'Identifier') return this.at({ root: source, path, aliases: lookup.aliases + 1 });
    const safe = source.type === 'ArrayExpression' && this.elementSafe(param, source, [undefined]);
    return safe ? found(leafStrings(source, path, this.rules(false))) : undefined;
  }

  private elementSource(binding: AstNode): AstNode | undefined {
    const fn = this.tree.parents.get(binding);
    const first = isFunction(fn) && (fn.params as AstNode[])[0] === binding;
    const call = first ? iterationCall(this.tree, fn!) : undefined;
    return call && ((call.callee as AstNode).object as AstNode);
  }

  private exported(binding: AstNode): boolean {
    const declarator = this.tree.parents.get(binding)!;
    const statement = this.tree.parents.get(declarator)!;
    if (typeOf(this.tree.parents.get(statement)) === 'ExportNamedDeclaration') return true;
    return this.tree.nodes.some((node) => node.type === 'ExportSpecifier' && specifierName(node.local) === binding.name);
  }

  private readsOf(binding: AstNode): Ref[] {
    return this.tree.reads(binding.name as string).filter((ref) => this.tree.bindingOf(ref.node) === binding);
  }

  private frozenLocally(binding: AstNode, table: AstNode): boolean {
    return this.readsOf(binding).every((ref) => this.safeRead(ref, table, []));
  }

  private safeRead(ref: Ref, table: AstNode, prefix: Path): boolean {
    if (typeOf(ref.parent) !== 'MemberExpression' || ref.field !== 'object') return false;
    return this.iterationRead(ref.parent!, table, prefix) || this.leafRead(ref.parent!, table, prefix);
  }

  private leafRead(member: AstNode, table: AstNode, prefix: Path): boolean {
    const outer = outermostMember(this.tree, member);
    if (isWrite(this.tree.ref(outer))) return false;
    return primitiveLeaves(table, [...prefix, ...memberPath(outer).path], this.rules(false));
  }

  private iterationRead(member: AstNode, table: AstNode, prefix: Path): boolean {
    const call = this.tree.parents.get(member)!;
    if (call.callee !== member) return false;
    const fn = (call.arguments as AstNode[])[0];
    if (!isFunction(fn) || iterationCall(this.tree, fn) !== call) return false;
    const param = (fn.params as AstNode[])[0];
    if (!param) return true;
    return nameOf(param) !== undefined && this.elementSafe(param, table, [...prefix, undefined]);
  }

  private elementSafe(param: AstNode, table: AstNode, prefix: Path): boolean {
    if (primitiveLeaves(table, prefix, this.rules(false))) return true;
    return this.readsOf(param).every((ref) => this.safeRead(ref, table, prefix));
  }
}
