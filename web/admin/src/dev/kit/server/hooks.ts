// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { openSessionCookie as openCookie } from '@bagel/kit/server/hooks';
import type { Session } from '#lib/server/session';
import { demoAdminIdentity } from '../../fixtures';

export * from '@bagel/kit/server/hooks';

function demoSession(): Session {
  const admin = demoAdminIdentity();
  const now = Math.floor(Date.now() / 1000);
  return { user_id: admin.id, login: admin.login, display_name: admin.display_name, role: 'streamer', iat: now, expires_at: now + 3600 };
}

export function openSessionCookie<S>(...args: Parameters<typeof openCookie<S>>): S | null {
  return openCookie(...args) ?? (demoSession() as S);
}
