import { json } from '@sveltejs/kit';
import { CURSOR_COOKIE, type Session } from '$lib/server/session';
import { setCursor } from '$lib/server/services';
import type { RequestHandler } from './$types';

// True for the genuine account owner: signed in, not a delegate, not an admin
// view-as. Only they may write the preference through to the account; everyone
// else gets the cookie-only local view.
function isOwner(s: Session | null): s is Session {
  return !!s?.user_id && !s.delegate_of && !s.impersonator_id;
}

// Custom-cursor preference sink. The settings + onboarding toggle flips the
// shared store for an instant live change and POSTs here to persist. We always
// set the preference cookie (fast, authoritative SSR on the next load for this
// browser) and, for a genuine account owner, write the choice through to the
// account so it follows them to another browser or device.
//
// A delegate or an admin view-as session only gets the cookie: their toggle is
// a local viewing preference and must never overwrite the owner's stored value.
export const POST: RequestHandler = async ({ request, url, cookies, locals }) => {
  const body = (await request.json().catch(() => null)) as { enabled?: unknown } | null;
  const on = body?.enabled === true;

  cookies.set(CURSOR_COOKIE, on ? '1' : '0', {
    path: '/',
    maxAge: 60 * 60 * 24 * 365,
    sameSite: 'lax',
    httpOnly: true,
    secure: url.protocol === 'https:'
  });

  const s = locals.session;
  if (isOwner(s)) {
    try {
      await setCursor(s.user_id, on);
    } catch {
      // The cookie already applied the choice on this device; only cross-device
      // sync failed, so report it without unwinding the local change.
      return json({ ok: false }, { status: 502 });
    }
  }
  return json({ ok: true });
};
