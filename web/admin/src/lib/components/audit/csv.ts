// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AuditEntry } from '$lib/server/services';
import { csvDocument } from '@bagel/ui/lib/csv';

const HEADER = 'id,actor_id,actor_login,action,target,detail,ok,error,created_at';

export function auditCsv(rows: readonly AuditEntry[]): string {
  return csvDocument(
    HEADER,
    rows.map((e) => [
      String(e.id),
      String(e.actor_id),
      e.actor_login,
      e.action,
      e.target ?? '',
      e.detail ?? '',
      String(e.ok),
      e.error ?? '',
      e.created_at
    ])
  );
}
