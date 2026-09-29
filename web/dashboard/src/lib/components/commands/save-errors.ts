// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { CommandErrors } from '@bagel/kit';

export interface SaveFailure {
  errors?: CommandErrors;
  code?: string;
  field?: 'name' | 'aliases';
  conflict?: string;
}

type Translate = (key: any, params?: Record<string, string | number>) => string;

export function saveFailureErrors(payload: SaveFailure | null | undefined, t: Translate): CommandErrors | null {
  if (!payload) return null;
  if (payload.errors) return payload.errors;
  const name = payload.conflict ?? '';
  if (payload.code === 'name_taken') return { name: t('commands.errNameTaken', { name }) };
  if (payload.code === 'builtin_name') return { name: t('commands.errBuiltinName') };
  if (payload.code === 'alias_taken') return { aliases: t('commands.errAliasTaken') };
  return null;
}

export const isUnavailable = (payload: SaveFailure | null | undefined) => payload?.code === 'unavailable';
