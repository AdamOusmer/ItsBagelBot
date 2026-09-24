// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import { auditAppend } from './services';
import type { AdminIdentity } from './access';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
const DEMO = dev && process.env.DEMO === '1';

export type AuditLine = {
  action: string;
  target: string;
  detail?: string;
  ok: boolean;
  error?: string;
};

export function audit(admin: AdminIdentity, line: AuditLine): void {
  if (DEMO) return;
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
