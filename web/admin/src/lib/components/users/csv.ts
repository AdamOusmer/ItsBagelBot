// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AdminUserWire } from '$lib/server/services';
import { csvDocument } from '@bagel/ui/lib/csv';

const HEADER = 'id,username,status,active,banned,creator_code,created_at,updated_at';

export function usersCsv(rows: readonly AdminUserWire[]): string {
  return csvDocument(
    HEADER,
    rows.map((u) => [
      String(u.id),
      u.username,
      u.status,
      String(u.is_active),
      String(u.banned),
      u.creator_code ?? '',
      u.created_at ?? '',
      u.updated_at ?? ''
    ])
  );
}
