// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Locale } from './types';

export type FlatTree = { [key: string]: string | FlatTree };
export type FlatCatalogs = Record<Locale, Record<string, string>>;

function namespaceOf(relativePath: string): string {
  const segments = relativePath.split('/');
  if (segments.at(-1) === 'index') segments.pop();
  return segments.join('.');
}

function flattenInto(tree: FlatTree, prefix: string, out: Record<string, string>): void {
  for (const [key, value] of Object.entries(tree)) {
    const full = prefix ? `${prefix}.${key}` : key;
    if (typeof value === 'string') out[full] = value;
    else flattenInto(value, full, out);
  }
}

export function flatCatalogs(files: Record<string, FlatTree>, surface: string): FlatCatalogs {
  const pattern = new RegExp(`(?:^|/)locales/([\\w-]+)/${surface}/(.+)\\.json$`);
  const catalogs: FlatCatalogs = {};
  for (const [path, tree] of Object.entries(files)) {
    const match = pattern.exec(path);
    if (!match) throw new Error(`i18n: ${path} is not a locales/<code>/${surface}/ catalog file`);
    flattenInto(tree, namespaceOf(match[2]), (catalogs[match[1]] ??= {}));
  }
  return catalogs;
}
