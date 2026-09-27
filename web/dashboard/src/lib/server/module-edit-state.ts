// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleView } from './commands-store';

function storedConfig(raw: unknown): Record<string, unknown> {
  return raw && typeof raw === 'object' ? raw as Record<string, unknown> : {};
}

function editableConfig(stored: Record<string, unknown>): Record<string, string> {
  const fields = Object.entries(stored).filter(([key]) => key !== '__rev');
  return Object.fromEntries(fields.map(([key, value]) => [key, value == null ? '' : String(value)]));
}

// The row revision is authoritative; __rev supports older service replies.
// Internal metadata must never become an editable setting.
export function moduleEditState(row?: ModuleView): { config: Record<string, string>; revision: number } {
  const stored = storedConfig(row?.configs);
  return { config: editableConfig(stored), revision: row?.revision ?? (Number(stored.__rev) || 0) };
}
