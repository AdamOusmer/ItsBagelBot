// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { error, type RequestEvent } from '@sveltejs/kit';
import { accountState } from '#lib/server/services';

// process.env, not $app/env/private: it only exposes variables declared in src/env.ts.
const DEMO = dev && process.env.DEMO === '1';

const ALPHA_ROUTE_PREFIX = '/(app)/overlays/';

function accountAlpha(userId: string): Promise<boolean> {
  return accountState(userId).then((acc) => acc.alpha).catch(() => false);
}

export async function broadcasterAlpha(locals: App.Locals): Promise<boolean> {
  if (DEMO) return (await import('#lib/server/demo-data')).demoAccountState.alpha;
  const s = locals.session;
  if (!s) return false;
  if (s.delegate_of) return accountAlpha(s.delegate_of);
  const cached = locals.accountState;
  if (cached && 'value' in cached) return cached.value.alpha;
  return accountAlpha(s.user_id);
}

export async function assertAlpha(event: RequestEvent): Promise<void> {
  if (!(await broadcasterAlpha(event.locals))) throw error(404);
}

export async function assertAlphaRouteOpen(event: RequestEvent): Promise<void> {
  if (event.route.id?.startsWith(ALPHA_ROUTE_PREFIX)) await assertAlpha(event);
}
