// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { auditAppend } from './services';
import type { AdminIdentity } from './access';

export type AuditLine = {
  action: string;
  target: string;
  detail?: string;
  ok: boolean;
  error?: string;
};

export function audit(admin: AdminIdentity, line: AuditLine): void {
  auditAppend({
    actor_id: admin.id,
    actor_login: admin.login,
    action: line.action,
    target: line.target,
    detail: line.detail ?? '',
    ok: line.ok,
    error: line.error
  }).catch(() => {});
}
