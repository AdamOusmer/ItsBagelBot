// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Handle, HandleServerError, RequestEvent, ServerInit } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import newrelic from 'newrelic';
import { COOKIE, open } from '$lib/server/session';
import { requireAdmin } from '$lib/server/access';
import { initConsoleRuntime } from '@bagel/kit/server/boot';
import {
  harden,
  noticeServerError,
  openSessionCookie,
  preloadStrategy,
  resolveLocale,
  tagTransaction
} from '@bagel/kit/server/hooks';
import { startInvalidationListener } from '$lib/server/services';
import { assertConfigSane } from '$lib/server/config-sanity';
import { ensureLaneStoreHA } from '$lib/server/lanes';
import { isPublicAdminRoute } from '$lib/server/public-routes';
import { countView, setAnalyticsApp } from '@bagel/kit/server/analytics';

setAnalyticsApp('admin');

const DEMO = dev && process.env.DEMO === '1';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
export const init: ServerInit = async () => {
  initConsoleRuntime(process.env, assertConfigSane);

  void ensureLaneStoreHA().catch((error) => {
    newrelic.noticeError(error instanceof Error ? error : new Error(String(error)), {
      component: 'nats-kv-ha-reconcile'
    });
  });

  startInvalidationListener();
};

type StaffAdmission = 'open' | 'signin' | 'denied';

// Page data requests can skip layout loads, so this hook is the only gate every route shares.
async function staffAdmission(event: RequestEvent): Promise<StaffAdmission> {
  if (DEMO || isPublicAdminRoute(event.url.pathname)) return 'open';
  if (!event.locals.session) return 'signin';
  return (await requireAdmin(event.locals.session)) ? 'open' : 'denied';
}

const PERMISSIONS_POLICY = 'camera=(), microphone=(), geolocation=(), payment=()';

export const handle: Handle = async ({ event, resolve }) => {
  event.locals.session = openSessionCookie(event, COOKIE, open);
  event.locals.locale = await resolveLocale(event);

  const admission = await staffAdmission(event);
  if (admission === 'signin') throw redirect(303, '/login');
  if (admission === 'denied') {
    event.cookies.delete(COOKIE, { path: '/', secure: event.url.protocol === 'https:' });
    event.locals.session = null;
    throw redirect(303, '/login?e=denied');
  }

  tagTransaction(newrelic, event, event.locals.session);
  countView(event);

  const res = await resolve(event, { preload: preloadStrategy });

  harden(res, PERMISSIONS_POLICY);
  return res;
};

export const handleError: HandleServerError = ({ error, event, status }) => {
  noticeServerError(newrelic, error, event, status);
  return { message: 'Internal Error' };
};
