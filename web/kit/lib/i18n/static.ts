// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import en from './locales/en.json';
import fr from './locales/fr.json';
import type { MessageTree } from './types';

const catalogs: Record<'en' | 'fr', MessageTree> = { en, fr };

type Node = string | string[] | MessageTree | undefined;

const isBranch = (node: Node): node is MessageTree => node != null && typeof node !== 'string' && !Array.isArray(node);

function lookup(tree: MessageTree | undefined, key: string): string | undefined {
  const node = key.split('.').reduce<Node>((current, part) => (isBranch(current) ? current[part] : undefined), tree);
  return typeof node === 'string' ? node : undefined;
}

export function staticText(locale: 'en' | 'fr', key: string): string {
  return lookup(catalogs[locale], key) ?? lookup(catalogs.en, key) ?? key;
}
