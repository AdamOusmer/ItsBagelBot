// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AdminUserWire } from '#lib/server/services';
import { csvDocument } from '@bagel/ui/lib/csv';

const HEADER = 'id,username,status,active,banned,creator_code,created_at,updated_at';

export function usersCsv(rows: readonly AdminUserWire[]): string {
  return csvDocument(
    HEADER,
    rows.map((user) => [
      String(user.id),
      user.username,
      user.status,
      String(user.is_active),
      String(user.banned),
      user.creator_code ?? '',
      user.created_at ?? '',
      user.updated_at ?? ''
    ])
  );
}
