// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { BUILTIN_NAMES } from '../../../../kit/lib/catalog/builtin-commands';
import type { CommandView } from '@bagel/kit';

export type SaveConflictCode = 'builtin_name' | 'name_taken' | 'alias_taken';

export interface SaveConflict {
  code: SaveConflictCode;
  name: string;
}

export interface SaveIntent {
  name: string;
  aliases: string[];
  isEdit: boolean;
  originalName: string;
}

function claimedNames(existing: CommandView[], ownName: string): Set<string> {
  const claimed = new Set<string>();
  for (const c of existing) {
    if (c.builtin || c.name === ownName) continue;
    claimed.add(c.name);
    for (const a of c.aliases ?? []) claimed.add(a);
  }
  return claimed;
}

function ownAliases(existing: CommandView[], ownName: string): Set<string> {
  return new Set(existing.find((c) => !c.builtin && c.name === ownName)?.aliases ?? []);
}

const ownNameOf = (intent: SaveIntent): string => (intent.isEdit ? intent.originalName || intent.name : '');

const takesNewName = (intent: SaveIntent, ownName: string): boolean => !intent.isEdit || ownName !== intent.name;

function aliasClash(aliases: string[], kept: Set<string>, claimed: Set<string>): string | undefined {
  const taken = (a: string) => claimed.has(a) || BUILTIN_NAMES.has(a);
  return aliases.find((a) => !kept.has(a) && taken(a));
}

export function saveConflict(intent: SaveIntent, existing: CommandView[]): SaveConflict | null {
  const { name } = intent;
  if (BUILTIN_NAMES.has(name)) return { code: 'builtin_name', name };

  const ownName = ownNameOf(intent);
  const claimed = claimedNames(existing, ownName);
  if (takesNewName(intent, ownName) && claimed.has(name)) return { code: 'name_taken', name };

  const clash = aliasClash(intent.aliases, ownAliases(existing, ownName), claimed);
  return clash ? { code: 'alias_taken', name: clash } : null;
}

export const conflictField = (code: SaveConflictCode): 'name' | 'aliases' => (code === 'alias_taken' ? 'aliases' : 'name');
