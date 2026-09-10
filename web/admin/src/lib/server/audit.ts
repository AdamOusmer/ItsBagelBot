// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The operator audit trail, written from one place.
//
// Seven route files had grown their own three-line `audit(...)` wrapper around
// auditAppend, each with a slightly different parameter order and its own idea
// of what an omitted detail is. That is the shape CodeScene flags as sibling
// duplication, and it is also how two of them ended up silently dropping the
// `detail` field. One helper, one line shape.
import { dev } from '$app/environment';
import { auditAppend } from './services';
import type { AdminIdentity } from './access';

// Build-time constant, branched on directly: see access.ts for why this is
// process.env and not $env/dynamic/private.
const DEMO = dev && process.env.DEMO === '1';

export type AuditLine = {
  action: string;
  target: string;
  detail?: string;
  ok: boolean;
  error?: string;
};

// audit records one mutating operator action, best effort: a logging failure
// must never block or fail the action it describes, so the promise is
// swallowed rather than awaited. Skipped under DEMO, whose synthetic identity
// carries a non-numeric actor id the users service would reject.
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
