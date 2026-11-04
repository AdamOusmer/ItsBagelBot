// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { readConsoleTree } from '../../../../kit/lib/i18n/tree-fs';
import type { MessageTree } from '../../../../kit/lib/i18n/types';
import type { Translate } from './messages';

const tree = readConsoleTree('en');

type TreeNode = string | string[] | MessageTree | undefined;

const isBranch = (node: TreeNode): node is MessageTree => typeof node === 'object' && !Array.isArray(node);

function lookup(key: string): string | undefined {
  let node: TreeNode = tree;
  for (const part of key.split('.')) {
    if (!isBranch(node)) return undefined;
    node = node[part];
  }
  return typeof node === 'string' ? node : undefined;
}

export const english: Translate = (key, params) => {
  let text = lookup(key) ?? key;
  for (const name in params) text = text.split(`{${name}}`).join(String(params[name]));
  return text;
};
