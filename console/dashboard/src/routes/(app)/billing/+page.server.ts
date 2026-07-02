import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import { billingState, checkoutBasketCreate, type BillingState } from '$lib/server/services';
import { env } from '$env/dynamic/private';

type BillingLinks = {
  checkoutUrl: string | null;
  cancelUrl: string | null;
};

function optionalHttpsURL(value: string | undefined): string | null {
  if (!value) return null;
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'https:' ? parsed.toString() : null;
  } catch {
    return null;
  }
}

function links(): BillingLinks {
  return {
    checkoutUrl: optionalHttpsURL(env.TEBEX_PREMIUM_CHECKOUT_URL),
    cancelUrl: optionalHttpsURL(env.TEBEX_CANCEL_URL)
  };
}

export const load: PageServerLoad = async ({ locals, url }) => {
  // ?subscribe=1 comes from the marketing site's pricing page (rides through
  // the login flow); the page auto-opens checkout when the plan allows it.
  const autostart = url.searchParams.get('subscribe') === '1';

  if (env.DEMO === '1') {
    return {
      account: {
        active: true,
        status: 'paid',
        expiresAt: new Date(Date.now() + 15 * 864e5).toISOString(),
        source: 'tebex'
      } as BillingState,
      links: {
        checkoutUrl: 'https://example.tebex.io/package/premium',
        cancelUrl: 'https://example.tebex.io/account'
      } satisfies BillingLinks,
      degraded: false,
      autostart
    };
  }

  const s = locals.session;
  // Owner-only: billing is never part of a delegated section grant.
  if (!s || s.delegate_of) throw redirect(302, '/');

  const accountResult = await billingState(s.user_id).then(
    (value) => ({ status: 'fulfilled' as const, value }),
    () => ({ status: 'rejected' as const })
  );

  return {
    account:
      accountResult.status === 'fulfilled'
        ? accountResult.value
        : ({ active: false, status: 'free', expiresAt: null, source: '' } as BillingState),
    links: links(),
    degraded: accountResult.status !== 'fulfilled',
    autostart
  };
};

export const actions: Actions = {
  // Mint a Tebex basket for this user (transactions service -> Headless API)
  // and hand the ident back so the page can launch the official Tebex.js
  // embedded checkout. If basket minting is down but a static hosted-checkout
  // URL is configured, fall back to the old 303 hand-off.
  subscribe: async ({ locals }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of || s.impersonator_id) {
      return fail(403, { error: 'Only the account owner can subscribe.' });
    }

    // Never send an already-premium user to Tebex: a staff-granted period,
    // active Tebex entitlement, or VIP grant must run out before a new charge is
    // possible.
    try {
      const state = await billingState(s.user_id);
      if (state.status !== 'free') {
        return fail(409, { error: 'You already have premium. Subscribing again is blocked so you are not double-charged.' });
      }
    } catch {
      return fail(502, { error: 'Could not verify your current plan. Try again in a moment.' });
    }

    try {
      const basket = await checkoutBasketCreate(s.user_id, s.login);
      return { ident: basket.ident, checkoutUrl: basket.checkoutUrl };
    } catch (err) {
      console.error('[billing] basket create failed:', err);
    }

    const url = links().checkoutUrl;
    if (!url) return fail(503, { error: 'Subscriptions are not available right now.' });
    throw redirect(303, url);
  },

  // Cancellation/account management lives on Tebex. We still gate the button
  // behind an owner session so delegated or view-as sessions cannot act on it.
  cancel: async ({ locals }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of || s.impersonator_id) {
      return fail(403, { error: 'Only the account owner can cancel the subscription.' });
    }

    const url = links().cancelUrl;
    if (!url) return fail(503, { error: 'Subscription management is not available right now.' });

    try {
      const state = await billingState(s.user_id);
      if (state.status !== 'paid' || state.source !== 'tebex') {
        return fail(409, { error: 'There is no Tebex subscription to cancel for this account.' });
      }
    } catch {
      return fail(502, { error: 'Could not verify your current plan. Try again in a moment.' });
    }

    throw redirect(303, url);
  }
};
