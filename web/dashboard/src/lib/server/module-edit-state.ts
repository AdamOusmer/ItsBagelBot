// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleView } from './commands-store';

// The row revision is authoritative; __rev supports older service replies.
// Internal metadata must never become an editable setting.
export function moduleEditState(row?: ModuleView): { config: Record<string, string>; revision: number } {
  const config: Record<string, string> = {};
  let mirror = 0;
  if (row?.configs && typeof row.configs === 'object') {
    for (const [key, value] of Object.entries(row.configs as Record<string, unknown>)) {
      if (key === '__rev') mirror = Number(value) || 0;
      else config[key] = value == null ? '' : String(value);
    }
  }
  return { config, revision: row?.revision ?? mirror };
}
