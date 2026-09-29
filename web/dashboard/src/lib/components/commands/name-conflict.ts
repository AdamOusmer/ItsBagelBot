// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { BUILTIN_NAMES } from '../../../../../kit/lib/catalog/builtin-commands';
import { normName } from '../../../../../kit/lib/engine/commands-validate';

export type NameConflict = { kind: 'builtin' } | { kind: 'taken'; name: string };

interface NameDraft {
  edit: boolean;
  name: string;
  originalName: string;
}

export function nameConflict(draft: NameDraft, customNames: readonly string[]): NameConflict | null {
  const name = normName(draft.name);
  if (!name) return null;
  if (BUILTIN_NAMES.has(name)) return { kind: 'builtin' };
  if (draft.edit && name === normName(draft.originalName)) return null;
  return customNames.includes(name) ? { kind: 'taken', name } : null;
}
