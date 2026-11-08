// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { error, fail, redirect, type ActionFailure } from '@sveltejs/kit';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import {
  parseAuthorizeParams,
  callbackUrl,
  type AuthorizeRequest
} from '#lib/server/device-auth-validate';
import { deviceCodeCreate } from '#lib/server/services';
import { checkConsentRate } from '#lib/server/device-auth-store';
import { safeNextPath } from '#lib/server/oauth';
import type { Session } from '#lib/server/session';

const DEMO = dev && env.DEMO === '1';

type SessionRefusal = 'impersonator' | 'delegate';

function sessionRefusal(s: Session): SessionRefusal | null {
  if (s.impersonator_id) return 'impersonator';
  if (s.delegate_of) return 'delegate';
  return null;
}

async function demoSession(): Promise<Session> {
  return (await import('#lib/server/demo-data')).demoSession();
}

async function currentSession(locals: App.Locals): Promise<Session | null> {
  if (locals.session) return locals.session;
  return DEMO ? demoSession() : null;
}

function invalidRequestRedirect(redirectUri: string, state: string | null): never {
  const target = callbackUrl(redirectUri, { error: 'invalid_request', ...(state ? { state } : {}) });
  throw redirect(302, target, { external: true });
}

export const load: PageServerLoad = async ({ url, locals }) => {
  const parsed = parseAuthorizeParams(url.searchParams);
  if (!parsed.ok && parsed.fatal) {
    error(400, 'This link is missing information or was altered and cannot be used.');
  }
  if (!parsed.ok) invalidRequestRedirect(parsed.redirectUri, parsed.state);

  const s = await currentSession(locals);
  if (!s) {
    const next = safeNextPath(url.pathname + url.search);
    throw redirect(302, next ? `/login?next=${encodeURIComponent(next)}` : '/login');
  }

  return {
    refusal: sessionRefusal(s),
    request: parsed.value,
    account: { login: s.login, displayName: s.display_name }
  };
};

function reparse(form: FormData): ReturnType<typeof parseAuthorizeParams> {
  const params = new URLSearchParams();
  for (const key of ['response_type', 'client_id', 'redirect_uri', 'code_challenge', 'code_challenge_method', 'dpop_jkt', 'state', 'device_name', 'platform']) {
    for (const value of form.getAll(key)) params.append(key, String(value));
  }
  return parseAuthorizeParams(params);
}

type Refusal = { ok: false; error: string };

async function gate(locals: App.Locals, form: FormData): Promise<
  | { ok: true; session: Session; request: AuthorizeRequest }
  | { ok: false; result: ActionFailure<Refusal> }
> {
  const parsed = reparse(form);
  if (!parsed.ok && parsed.fatal) error(400, 'This link is missing information or was altered and cannot be used.');
  if (!parsed.ok) invalidRequestRedirect(parsed.redirectUri, parsed.state);

  const s = await currentSession(locals);
  if (!s) return { ok: false, result: fail(401, { ok: false, error: 'Sign in again to finish this.' }) };
  if (sessionRefusal(s)) return { ok: false, result: fail(403, { ok: false, error: 'This session cannot approve a device.' }) };

  return { ok: true, session: s, request: parsed.value };
}

export const actions: Actions = {
  approve: async ({ request, locals }) => {
    const gated = await gate(locals, await request.formData());
    if (!gated.ok) return gated.result;
    const { session: s, request: r } = gated;

    const limited = await checkConsentRate(s.user_id);
    if (!limited.allowed) return fail(429, { ok: false, error: 'Too many attempts. Wait a moment and try again.' });

    let code: string;
    try {
      code = DEMO
        ? `demo-code-${Date.now()}`
        : await deviceCodeCreate({
            userId: s.user_id,
            codeChallenge: r.codeChallenge,
            dpopJkt: r.dpopJkt,
            redirectUri: r.redirectUri,
            deviceName: r.deviceName,
            platform: r.platform
          });
    } catch {
      // A failed call carries no state to redirect with: ask the user to retry here instead.
      return fail(502, { ok: false, error: 'Could not reach the account service. Try again in a moment.' });
    }

    const target = callbackUrl(r.redirectUri, { code, state: r.state });
    return { ok: true, action: 'approved' as const, target };
  },

  cancel: async ({ request, locals }) => {
    const gated = await gate(locals, await request.formData());
    if (!gated.ok) return gated.result;
    const { session: s, request: r } = gated;

    const limited = await checkConsentRate(s.user_id);
    if (!limited.allowed) return fail(429, { ok: false, error: 'Too many attempts. Wait a moment and try again.' });

    const target = callbackUrl(r.redirectUri, { error: 'access_denied', state: r.state });
    return { ok: true, action: 'cancelled' as const, target };
  }
};
