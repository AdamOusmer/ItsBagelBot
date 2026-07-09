// Redeem an admin "view as" link: verify the signed token, burn its jti (a
// link is single-use), then seal a short (1h) dashboard session for the target
// user that also carries the acting admin (impersonator_*). hooks.server.ts
// opens it like any session — with a hard 1h cap from iat — and the write
// actions audit back to the admin while these fields are present.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import newrelic from 'newrelic';
import { verifyViewAs } from '@bagel/shared/server/impersonation';
import { claimOnce } from '@bagel/shared/server/rate-limit';
import { COOKIE, seal, IMPERSONATION_TTL_SECONDS } from '$lib/server/session';
import { LOCALE_COOKIE } from '@bagel/shared/i18n';

// jti claims only need to outlive the token's own 5-minute validity window
// (plus clock skew); after that verifyViewAs rejects the token anyway.
const JTI_TTL_SECONDS = 6 * 60;

export const GET: RequestHandler = async ({ url, cookies }) => {
  const token = url.searchParams.get('t') ?? '';
  const p = verifyViewAs(token);
  if (!p) throw redirect(302, '/login?e=imp');

  // Single-use gate. Only a proven replay is rejected. The jti burn is
  // defense-in-depth behind the token's own controls (5-min TTL + single-actor
  // Ed25519 signature), so it degrades OPEN when the Valkey write path is down:
  // Valkey is never a hard dependency elsewhere (readyz doesn't gate it, reads
  // fall back to RPC), and failing closed here turned a write-path blip into a
  // total outage of an admin-only, audited surface. The residual risk is a
  // link replayable for <=5 min only while the write path is unreachable.
  // 'unconfigured' (dev, no Valkey) also passes so local flows work. We still
  // tag 'unavailable' so a persistent write-path outage stays visible.
  const claim = await claimOnce(`viewas:jti:${p.jti}`, JTI_TTL_SECONDS);
  if (claim === 'replayed' || claim === 'unavailable') {
    newrelic.addCustomAttributes({ 'viewas.claim': claim, 'viewas.by': p.by_id });
  }
  if (claim === 'replayed') {
    throw redirect(302, '/login?e=imp');
  }

  const now = Math.floor(Date.now() / 1000);
  const value = seal({
    user_id: p.sub,
    login: p.login,
    display_name: p.display_name,
    role: 'streamer',
    iat: now,
    expires_at: now + IMPERSONATION_TTL_SECONDS,
    impersonator_id: p.by_id,
    impersonator_login: p.by_login
  });

  cookies.set(COOKIE, value, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: IMPERSONATION_TTL_SECONDS
  });

  // Impersonation is an admin surface: keep its chrome in English regardless
  // of the target account's preference. The Settings switch still writes the
  // target's saved locale through /lang.
  cookies.set(LOCALE_COOKIE, 'en', {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: 60 * 60 * 24 * 365
  });

  throw redirect(302, '/');
};
