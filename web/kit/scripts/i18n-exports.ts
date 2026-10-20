// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { Tree, isNode, leafStrings, literalString, nameOf, specifierName, typeOf, type AstNode, type Path, type TableRules } from './i18n-ast.ts';

export interface ExportTarget {
  local?: string;
  source?: string;
  imported?: string;
  stars: string[];
}

const TOP_LEVEL = new Set(['Program', 'ExportNamedDeclaration']);

function declaredNames(declaration: AstNode): string[] {
  const declarators = (declaration.declarations as AstNode[] | undefined) ?? [];
  return [nameOf(declaration.id), ...declarators.map((declarator) => nameOf(declarator.id))].filter((name): name is string => !!name);
}

function exportedDeclaration(tree: Tree, name: string): string | undefined {
  const declarations = tree.nodes.filter((node) => node.type === 'ExportNamedDeclaration' && isNode(node.declaration));
  return declarations.some((node) => declaredNames(node.declaration as AstNode).includes(name)) ? name : undefined;
}

function importOf(tree: Tree, local: string): { source?: string; imported?: string } {
  const specifier = tree.nodes.find((node) => node.type === 'ImportSpecifier' && nameOf(node.local) === local);
  if (!specifier) return {};
  return { source: literalString(tree.parents.get(specifier)!.source), imported: specifierName(specifier.imported) };
}

function specifierTarget(tree: Tree, specifier: AstNode, stars: string[]): ExportTarget {
  const local = specifierName(specifier.local)!;
  const source = literalString(tree.parents.get(specifier)!.source);
  if (source) return { source, imported: local, stars };
  return { ...importOf(tree, local), local, stars };
}

/** Where a module's export of `name` comes from: a local binding, a re-export, or one of its star re-exports. */
export function exportTarget(program: AstNode, name: string): ExportTarget {
  const tree = new Tree(program);
  const stars = tree.nodes.filter((node) => node.type === 'ExportAllDeclaration' && !node.exported).map((node) => literalString(node.source) ?? '');
  const declared = exportedDeclaration(tree, name);
  if (declared) return { local: declared, stars };
  const specifier = tree.nodes.find((node) => node.type === 'ExportSpecifier' && specifierName(node.exported) === name);
  return specifier ? specifierTarget(tree, specifier, stars) : { stars };
}

function isTopLevel(tree: Tree, binding: AstNode): boolean {
  const parent = tree.parents.get(binding);
  const statement = typeOf(parent) === 'VariableDeclarator' ? tree.parents.get(parent!) : parent;
  return TOP_LEVEL.has(typeOf(tree.parents.get(statement!)));
}

function initOf(tree: Tree, parent: AstNode | undefined): AstNode | undefined {
  if (typeOf(parent) === 'FunctionDeclaration') return parent;
  const constant = typeOf(parent) === 'VariableDeclarator' && tree.parents.get(parent!)!.kind === 'const';
  return constant ? (parent!.init as AstNode) : undefined;
}

/** The value a module-level, never reassigned `const` or function declaration named `local` holds. */
export function declarationOf(tree: Tree, local: string): AstNode | undefined {
  const declared = tree.declarations(local).filter((id) => isTopLevel(tree, id));
  if (declared.length !== 1 || tree.isWritten(local)) return undefined;
  return initOf(tree, tree.parents.get(declared[0]));
}

export function exportedTableStrings(program: AstNode, local: string, path: Path): string[] | undefined {
  const tree = new Tree(program);
  const init = declarationOf(tree, local);
  const rules: TableRules = { frozen: true, spread: (id) => declarationOf(tree, id.name as string) };
  return init && leafStrings(init, path, rules);
}
