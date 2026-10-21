// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Tree, contains, isFunction, literalString, nameOf, propertyName, specifierName, typeOf, type AstNode, type Ref } from './i18n-ast.ts';
import { declarationOf } from './i18n-exports.ts';
import { KeyResolver, isBounded, refAt, type Deferred, type Pattern, type Resolution, type TranslatorRole } from './i18n-resolve.ts';
import type { ImportedName } from './i18n-tables.ts';

export { exportTarget, exportedTableStrings } from './i18n-exports.ts';
export { isBounded, patternRegExp, type Deferred, type Pattern, type TranslatorRole } from './i18n-resolve.ts';

export interface Usage {
  patterns: Pattern[];
  problems: string[];
  deferred: Deferred[];
  namespaces: string[];
}

type Role = 'factory' | 'object' | 'namespace' | TranslatorRole;

interface Binding {
  role: Role;
  start: number;
  end: number;
}

const FACTORIES = new Set(['getI18n', 'setI18n']);
const RAW = new Set(['translate', 'translateList']);
export const I18N_API = new Set([...FACTORIES, ...RAW]);
const TRANSLATOR_MEMBERS = new Set(['t', 'tl']);
const TEST_FIELDS: Record<string, string> = { ConditionalExpression: 'test', IfStatement: 'test' };
const TEST_OPERATORS = new Set(['!', 'typeof']);

function isTruthinessTest({ parent, field }: Ref): boolean {
  const type = typeOf(parent);
  if (TEST_FIELDS[type]) return TEST_FIELDS[type] === field;
  if (type === 'LogicalExpression') return parent!.operator === '&&' && field === 'left';
  return type === 'UnaryExpression' && TEST_OPERATORS.has(parent!.operator as string);
}

const roleOf = (imported: string): Role => (FACTORIES.has(imported) ? 'factory' : 'raw');

function isKeySet(found: Resolution | undefined): found is Resolution {
  if (!found) return false;
  const named = found.patterns.length > 0 || found.deferred.length > 0;
  return named && found.patterns.every(isBounded);
}

function destructuredName(property: AstNode): string | undefined {
  return property.type === 'Property' && !property.computed ? nameOf(property.key) : undefined;
}

class Analyzer {
  private readonly bindings = new Map<string, Binding[]>();
  private readonly imports = new Map<string, ImportedName>();
  private readonly keys: KeyResolver;
  private readonly visited = new Set<AstNode>();
  readonly usage: Usage = { patterns: [], problems: [], deferred: [], namespaces: [] };

  constructor(
    private readonly tree: Tree,
    private readonly code: string
  ) {
    for (const node of tree.nodes) if (node.type === 'ImportDeclaration') this.collectImport(node);
    this.keys = new KeyResolver(tree, this.imports, (node) => this.snippet(node));
  }

  module(): void {
    for (const [local, { imported }] of this.imports) if (I18N_API.has(imported)) this.bind(local, roleOf(imported));
    for (const node of this.tree.nodes) if (node.type === 'ExportSpecifier') this.checkExport(node);
    this.discoverBindings();
    for (const node of this.tree.nodes) if (this.isFactoryCall(node)) this.factoryResult(node);
    this.checkReferences(this.tree.root);
  }

  functionParam(fn: AstNode, index: number, role: TranslatorRole): void {
    if (this.visited.has(fn)) return;
    this.visited.add(fn);
    const name = nameOf((fn.params as AstNode[])[index]);
    if (!name) return this.problem('translator parameter is not a plain name', fn);
    this.bind(name, role, fn);
    this.checkReferences(fn);
  }

  private snippet(node: AstNode): string {
    return this.code.slice(node.start, node.end).replace(/\s+/g, ' ').slice(0, 140);
  }

  private problem(kind: string, node: AstNode): void {
    this.usage.problems.push(`${kind}: ${this.snippet(node)}`);
  }

  private bind(name: string, role: Role, range: AstNode = this.tree.root): void {
    this.bindings.set(name, [...(this.bindings.get(name) ?? []), { role, start: range.start, end: range.end }]);
  }

  private roleAt(id: AstNode): Role | undefined {
    const inRange = (this.bindings.get(id.name as string) ?? []).filter((binding) => contains(binding, id));
    const innermost = inRange.sort((a, b) => a.end - a.start - (b.end - b.start))[0];
    return innermost && innermost.role;
  }

  private collectImport(declaration: AstNode): void {
    const source = literalString(declaration.source) ?? '';
    for (const specifier of declaration.specifiers as AstNode[]) this.recordImport(specifier, source);
  }

  private recordImport(specifier: AstNode, source: string): void {
    const local = nameOf(specifier.local) ?? '';
    if (specifier.type === 'ImportNamespaceSpecifier') return this.bind(local, 'namespace');
    const imported = specifier.type === 'ImportSpecifier' ? (specifierName(specifier.imported) ?? '') : 'default';
    this.imports.set(local, { source, imported });
  }

  private checkExport(node: AstNode): void {
    const local = specifierName(node.local) ?? '';
    if (I18N_API.has(local) && local !== specifierName(node.exported)) this.problem('renamed i18n export', node);
  }

  private isFactoryCall(node: AstNode): boolean {
    const callee = node.callee as AstNode;
    return node.type === 'CallExpression' && callee.type === 'Identifier' && this.roleAt(callee) === 'factory';
  }

  private discoverBindings(): void {
    const bound = new Set<AstNode>();
    let pending = this.pendingDeclarators(bound);
    while (pending.length) {
      for (const declarator of pending) {
        bound.add(declarator);
        this.bindDeclarator(declarator);
      }
      pending = this.pendingDeclarators(bound);
    }
  }

  private pendingDeclarators(bound: Set<AstNode>): AstNode[] {
    return this.tree.nodes.filter((node) => this.bindsObject(node)).map((node) => this.tree.parents.get(node)!).filter((declarator) => !bound.has(declarator));
  }

  private bindsObject(node: AstNode): boolean {
    if (!refAt(this.tree.ref(node), 'VariableDeclarator', 'init')) return false;
    return this.isFactoryCall(node) || (node.type === 'Identifier' && this.roleAt(node) === 'object');
  }

  private bindDeclarator(declarator: AstNode): void {
    const id = declarator.id as AstNode;
    if (id.type === 'Identifier') this.bind(id.name as string, 'object');
    else if (id.type === 'ObjectPattern') this.destructure(id);
    else this.problem('i18n object escapes', declarator);
  }

  private destructure(pattern: AstNode): void {
    for (const property of pattern.properties as AstNode[]) this.destructureProperty(property);
  }

  private destructureProperty(property: AstNode): void {
    const name = destructuredName(property);
    const local = nameOf(property.value);
    if (name === 'locale') return;
    if (TRANSLATOR_MEMBERS.has(name ?? '') && local) this.bind(local, 'translator');
    else this.problem('unsupported i18n destructuring', property);
  }

  private factoryResult(call: AstNode): void {
    const ref = this.tree.ref(call);
    if (refAt(ref, 'VariableDeclarator', 'init')) return;
    if (refAt(ref, 'MemberExpression', 'object')) return this.memberUse(ref.parent!);
    if (typeOf(ref.parent) !== 'ExpressionStatement') this.problem('i18n object escapes', ref.parent ?? call);
  }

  private checkReferences(within: AstNode): void {
    for (const id of this.tree.identifiers.filter((node) => contains(within, node))) this.checkIdentifier(id);
  }

  private checkIdentifier(id: AstNode): void {
    const role = this.roleAt(id);
    const ref = this.tree.ref(id);
    if (role && this.tree.isRead(ref)) this.checkReference(ref, role);
  }

  private readonly referenceChecks: Record<Role, (ref: Ref) => void> = {
    factory: (ref) => this.factoryReference(ref),
    object: (ref) => this.objectReference(ref),
    namespace: (ref) => this.namespaceReference(ref),
    translator: (ref) => this.translatorReference(ref, 'translator'),
    raw: (ref) => this.translatorReference(ref, 'raw')
  };

  private checkReference(ref: Ref, role: Role): void {
    this.referenceChecks[role](ref);
  }

  private factoryReference(ref: Ref): void {
    if (!refAt(ref, 'CallExpression', 'callee')) this.problem('i18n factory escapes', ref.parent ?? ref.node);
  }

  private namespaceReference(ref: Ref): void {
    const member = refAt(ref, 'MemberExpression', 'object') ? propertyName(ref.parent!) : undefined;
    if (member === undefined) this.usage.namespaces.push(this.namespaceSource(ref.node));
    else if (I18N_API.has(member)) this.problem('namespace i18n access', ref.parent!);
  }

  private namespaceSource(local: AstNode): string {
    const specifier = this.tree.nodes.find((node) => node.type === 'ImportNamespaceSpecifier' && nameOf(node.local) === local.name);
    return literalString(this.tree.parents.get(specifier!)!.source) ?? '';
  }

  private objectReference(ref: Ref): void {
    if (refAt(ref, 'MemberExpression', 'object')) return this.memberUse(ref.parent!);
    if (!refAt(ref, 'VariableDeclarator', 'init')) this.problem('i18n object escapes', ref.parent ?? ref.node);
  }

  private memberUse(member: AstNode): void {
    const name = propertyName(member) ?? '';
    if (name === 'locale') return;
    if (TRANSLATOR_MEMBERS.has(name)) return this.translatorReference(this.tree.ref(member), 'translator');
    this.problem('unsupported i18n member', member);
  }

  private translatorReference(ref: Ref, role: TranslatorRole): void {
    if (isTruthinessTest(ref)) return;
    if (refAt(ref, 'CallExpression', 'callee')) return this.keySite(ref.parent!, role);
    if (refAt(ref, 'CallExpression', 'arguments')) return this.passTranslator(ref.parent!, ref.node, role);
    this.problem('translator escapes', ref.parent ?? ref.node);
  }

  private keySite(call: AstNode, role: TranslatorRole): void {
    if (!this.tree.insideFunction(call)) this.problem('module-scope translation', call);
    const arg = (call.arguments as AstNode[])[role === 'raw' ? 1 : 0];
    const found = arg && this.keys.resolve(arg);
    if (!isKeySet(found)) return this.problem('unbounded key', call);
    this.usage.patterns.push(...found.patterns);
    this.usage.deferred.push(...found.deferred);
  }

  private passTranslator(call: AstNode, arg: AstNode, role: TranslatorRole): void {
    const callee = call.callee as AstNode;
    const index = (call.arguments as AstNode[]).indexOf(arg);
    const imported = this.importedCallee(callee);
    if (imported) return void this.usage.deferred.push({ kind: 'call', ...imported, index, role, path: [], site: this.snippet(call) });
    const fn = this.localFunction(callee);
    if (fn) this.functionParam(fn, index, role);
    else this.problem('translator escapes', call);
  }

  private importedCallee(callee: AstNode): ImportedName | undefined {
    if (callee.type !== 'Identifier' || this.tree.bindingOf(callee)) return undefined;
    return this.imports.get(callee.name as string);
  }

  private localFunction(callee: AstNode): AstNode | undefined {
    const binding = callee.type === 'Identifier' ? this.tree.constantBinding(callee) : undefined;
    const parent = binding && this.tree.parents.get(binding);
    if (!parent || parent.id !== binding) return undefined;
    const fn = parent.type === 'VariableDeclarator' ? parent.init : parent;
    return isFunction(fn) ? fn : undefined;
  }
}

export function analyzeModule(program: AstNode, code: string): Usage {
  const analyzer = new Analyzer(new Tree(program), code);
  analyzer.module();
  return analyzer.usage;
}

export function analyzeExportedFunction(program: AstNode, code: string, local: string, item: Deferred): Usage {
  const tree = new Tree(program);
  const fn = declarationOf(tree, local);
  const analyzer = new Analyzer(tree, code);
  if (isFunction(fn)) analyzer.functionParam(fn, item.index, item.role);
  else analyzer.usage.problems.push(`translator passed to a non-function export ${local}: ${item.site}`);
  return analyzer.usage;
}
