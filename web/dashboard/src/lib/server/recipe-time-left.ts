// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { leftCommandNames, normalizeCommandName } from '@bagel/kit/automations';
import { listAutomations } from './automations-store';

export async function recipeTimeLeftNames(userId: string): Promise<Set<string>> {
  const rows = await listAutomations(userId);
  return new Set(rows.flatMap((row) => leftCommandNames(row.definition)).map(normalizeCommandName));
}
