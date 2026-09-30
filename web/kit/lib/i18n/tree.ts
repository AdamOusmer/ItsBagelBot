// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Locale, MessageTree } from './types';

export interface CatalogFile {
  path: string;
  content: unknown;
}

const CONSOLE_FILE = /(?:^|\/)([\w-]+)\/console\/(.+)\.json$/;

const own = (tree: object, key: string): boolean => Object.prototype.hasOwnProperty.call(tree, key);

const isBranch = (node: unknown): node is Record<string, unknown> =>
  typeof node === 'object' && node !== null && !Array.isArray(node);

export function catalogLocale(path: string): Locale | undefined {
  return CONSOLE_FILE.exec(path)?.[1];
}

function keyPrefix(path: string): { locale: Locale; prefix: string[] } {
  const match = CONSOLE_FILE.exec(path);
  if (!match) throw new Error(`i18n: ${path} is not a locales/<code>/console/ catalog file`);
  const prefix = match[2].split('/');
  if (prefix[prefix.length - 1] === 'index') prefix.pop();
  return { locale: match[1], prefix };
}

function branchAt(root: MessageTree, path: string[], source: string): MessageTree {
  let node = root;
  for (const part of path) {
    if (!own(node, part)) node[part] = {};
    const next = node[part];
    if (!isBranch(next)) throw new Error(`i18n: ${source} overlaps the key ${path.join('.')}`);
    node = next;
  }
  return node;
}

function mergeFile(root: MessageTree, prefix: string[], content: unknown, source: string): void {
  if (!isBranch(content)) throw new Error(`i18n: ${source} must hold a JSON object`);
  for (const [key, value] of Object.entries(content)) {
    const path = [...prefix, ...key.split('.')];
    if (isBranch(value)) {
      mergeFile(root, path, value, source);
      continue;
    }
    const leaf = path.pop() as string;
    const parent = branchAt(root, path, source);
    if (own(parent, leaf)) throw new Error(`i18n: ${source} redefines ${[...path, leaf].join('.')}`);
    parent[leaf] = value as string | string[];
  }
}

const byPath = (a: CatalogFile, b: CatalogFile): number => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0);

export function assembleCatalogs(files: readonly CatalogFile[]): Record<Locale, MessageTree> {
  const trees: Record<Locale, MessageTree> = {};
  for (const file of [...files].sort(byPath)) {
    const { locale, prefix } = keyPrefix(file.path);
    trees[locale] ??= {};
    mergeFile(trees[locale], prefix, file.content, file.path);
  }
  return trees;
}
