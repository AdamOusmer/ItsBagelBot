// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import type { Session } from '../session';
import { SUB } from './subjects';

export type AuditEntry = {
  actorId: string;
  actorLogin: string;
  action: string;
  target: string;
  detail: string;
};

export async function auditImpersonation(entry: AuditEntry): Promise<void> {
  try {
    await rpc(`${SUB.audit}.append`, {
      actor_id: entry.actorId,
      actor_login: entry.actorLogin,
      action: entry.action,
      target: entry.target,
      detail: entry.detail,
      ok: true,
      error: ''
    });
  } catch {}
}

export function auditDashboardImpersonation(
  session: Session | null | undefined,
  action: string,
  detail = ''
): void {
  if (!session?.impersonator_id) return;
  auditImpersonation({
    actorId: session.impersonator_id,
    actorLogin: session.impersonator_login ?? '',
    action: `dashboard:${action}`,
    target: session.user_id,
    detail
  });
}
