// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AdminUserWire } from '$lib/server/services';

const HEADER = 'id,username,status,active,banned,creator_code,created_at,updated_at';

// RFC 4180 quoting: a field containing a quote, comma or newline is wrapped and
// its own quotes doubled. Spelled out rather than joined naively because a
// creator code is operator-supplied text and a comma in one used to shift every
// column after it by one.
function cell(v: string): string {
  return /[",\n]/.test(v) ? `"${v.replaceAll('"', '""')}"` : v;
}

/** The loaded page as CSV. The state filter is applied server-side, so this is
 *  exactly the rows on screen. */
export function usersCsv(rows: readonly AdminUserWire[]): string {
  const lines = rows.map((u) =>
    [
      String(u.id),
      u.username,
      u.status,
      String(u.is_active),
      String(u.banned),
      u.creator_code ?? '',
      u.created_at ?? '',
      u.updated_at ?? ''
    ]
      .map(cell)
      .join(',')
  );
  return [HEADER, ...lines].join('\n');
}
