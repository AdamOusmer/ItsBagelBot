// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { consoleLocales, readConsoleTree } from './tree-fs';
import type { Locale } from './types';
import type { MessageTree } from './types';

export const STATIC_LOCALES: readonly Locale[] = Object.freeze(consoleLocales());
const catalogs: Record<Locale, MessageTree> = Object.fromEntries(STATIC_LOCALES.map((locale) => [locale, readConsoleTree(locale)]));

type Node = string | string[] | MessageTree | undefined;

const isBranch = (node: Node): node is MessageTree => node != null && typeof node !== 'string' && !Array.isArray(node);

function lookup(tree: MessageTree | undefined, key: string): string | undefined {
  const node = key.split('.').reduce<Node>((current, part) => (isBranch(current) ? current[part] : undefined), tree);
  return typeof node === 'string' ? node : undefined;
}

export function staticText(locale: Locale, key: string): string {
  return lookup(catalogs[locale], key) ?? lookup(catalogs.en, key) ?? key;
}
