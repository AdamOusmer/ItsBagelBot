// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Handle, HandleServerError, RequestEvent, ServerInit } from '@sveltejs/kit';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import newrelic from 'newrelic';
import { COOKIE, open } from '$lib/server/session';
import { requireAdmin, requireRole } from '$lib/server/access';
import { initConsoleRuntime } from '@bagel/kit/server/boot';
import {
  harden,
  noticeServerError,
  openSessionCookie,
  preloadStrategy,
  tagTransaction
} from '@bagel/kit/server/hooks';
import { rumTransform } from '@bagel/kit/server/rum';
import { detectLocale, isLocale, LOCALE_COOKIE, ensureCatalog } from '@bagel/kit/i18n';
import { startInvalidationListener } from '$lib/server/services';
import { assertConfigSane } from '$lib/server/config-sanity';
import { ensureLaneStoreHA } from '$lib/server/lanes';

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

// Not all of '/auth': /auth/bot/* writes the bot's live Twitch token and is owner-gated below.
const PUBLIC_PREFIXES = [
  '/auth/login',
  '/auth/callback',
  '/auth/logout',
  '/login',
  '/healthz',
  '/readyz'
];

const BOT_FLOW_PREFIX = '/auth/bot';

function matches(pathname: string, prefix: string): boolean {
  return pathname === prefix || pathname.startsWith(prefix + '/');
}

function isPublic(pathname: string): boolean {
  return PUBLIC_PREFIXES.some((p) => matches(pathname, p));
}

async function staleStaffSession(event: RequestEvent): Promise<boolean> {
  if (DEMO || isPublic(event.url.pathname)) return false;
  if (!event.locals.session) return false;
  return !(await requireAdmin(event.locals.session));
}

async function botFlowRefused(event: RequestEvent): Promise<boolean> {
  if (DEMO || !matches(event.url.pathname, BOT_FLOW_PREFIX)) return false;
  return !(await requireRole(event, 'bot.token'));
}

function resolveLocale(event: RequestEvent): ReturnType<typeof detectLocale> {
  const queryLang = event.url.searchParams.get('lang');
  if (isLocale(queryLang)) {
    event.cookies.set(LOCALE_COOKIE, queryLang, { path: '/', maxAge: 31536000, secure: true, sameSite: 'lax' });
  }
  return detectLocale({
    cookie: queryLang || event.cookies.get(LOCALE_COOKIE),
    accept: event.request.headers.get('accept-language')
  });
}

const PERMISSIONS_POLICY = 'camera=(), microphone=(), geolocation=(), payment=()';

export const handle: Handle = async ({ event, resolve }) => {
  event.locals.session = openSessionCookie(event, COOKIE, open);
  event.locals.locale = resolveLocale(event);
  await ensureCatalog(event.locals.locale);

  if (await staleStaffSession(event)) {
    event.cookies.delete(COOKIE, { path: '/', secure: event.url.protocol === 'https:' });
    event.locals.session = null;
    throw redirect(303, '/login?e=denied');
  }

  if (await botFlowRefused(event)) throw redirect(303, '/login?e=denied');

  tagTransaction(newrelic, event, event.locals.session);

  const res = await resolve(event, {
    preload: preloadStrategy,
    transformPageChunk: rumTransform()
  });

  harden(res, PERMISSIONS_POLICY);
  return res;
};

export const handleError: HandleServerError = ({ error, event, status }) => {
  noticeServerError(newrelic, error, event, status);
  return { message: 'Internal Error' };
};
