// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error, type RequestEvent } from '@sveltejs/kit';
import { accountState } from '#lib/server/services';

const ALPHA_ROUTE_PREFIX = '/(app)/overlays/';

function accountAlpha(userId: string): Promise<boolean> {
  return accountState(userId).then((acc) => acc.alpha).catch(() => false);
}

export async function broadcasterAlpha(locals: App.Locals): Promise<boolean> {
  const s = locals.session;
  if (!s) return false;
  if (s.delegate_of) return accountAlpha(s.delegate_of);
  const cached = locals.accountState;
  if (cached && 'value' in cached) return cached.value.alpha;
  return accountAlpha(s.user_id);
}

export async function assertAlpha(event: { locals: App.Locals }): Promise<void> {
  if (!(await broadcasterAlpha(event.locals))) throw error(404);
}

export function alphaOnly<H extends (event: never) => unknown>(handler: H): H {
  return (async (event: RequestEvent) => {
    await assertAlpha(event);
    return handler(event as never);
  }) as unknown as H;
}

export function alphaActions<T extends Record<string, (event: never) => unknown>>(actions: T): T {
  return Object.fromEntries(Object.entries(actions).map(([name, action]) => [name, alphaOnly(action)])) as T;
}

export async function assertAlphaRouteOpen(event: RequestEvent): Promise<void> {
  if (event.route.id?.startsWith(ALPHA_ROUTE_PREFIX)) await assertAlpha(event);
}
