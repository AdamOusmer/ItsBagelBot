import { redirect } from '@sveltejs/kit';
import { isLocale, LOCALE_COOKIE } from '@bagel/shared/i18n';
import { setLocale } from '$lib/server/services';
import type { RequestHandler } from './$types';

// Locale switcher target. A plain form POST (no fetch, so it survives the strict
// connect-src CSP) sets the preference cookie and bounces back to where the user
// was. The cookie is what hooks.server.ts reads on the next request; for a
// signed-in user the choice is also persisted to their account so it follows
// them to another browser or device.
export const POST: RequestHandler = async ({ request, url, cookies, locals }) => {
  const form = await request.formData();
  const to = String(form.get('to') ?? '');
  const next = String(form.get('next') ?? '/');

  if (isLocale(to)) {
    cookies.set(LOCALE_COOKIE, to, {
      path: '/',
      maxAge: 60 * 60 * 24 * 365,
      sameSite: 'lax',
      httpOnly: true,
      secure: url.protocol === 'https:'
    });

    // Persist to the account — but not while an admin is impersonating (the
    // cookie already flips the current view; writing would change the *target*
    // user's saved preference). Best-effort: the cookie is authoritative for
    // rendering, so an RPC hiccup must not fail the switch.
    const s = locals.session;
    if (s?.user_id && !s.impersonator_id) {
      try {
        await setLocale(s.user_id, to);
      } catch {
        /* cookie already set; DB will catch up on the next change */
      }
    }
  }

  // Same-origin relative paths only — never honour an absolute/protocol-relative
  // `next`, which would turn this into an open redirect.
  const dest = next.startsWith('/') && !next.startsWith('//') ? next : '/';
  throw redirect(303, dest);
};
