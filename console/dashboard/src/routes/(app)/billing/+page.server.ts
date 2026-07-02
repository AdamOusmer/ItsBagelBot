import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import { billingState, type BillingState } from '$lib/server/services';
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

export const load: PageServerLoad = async ({ locals }) => {
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
      degraded: false
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
    degraded: accountResult.status !== 'fulfilled'
  };
};

export const actions: Actions = {
  // Send the browser to the Tebex checkout selected by our dashboard UI. 303 so
  // the POST becomes a GET on the external payment page.
  subscribe: async ({ locals }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of || s.impersonator_id) {
      return fail(403, { error: 'Only the account owner can subscribe.' });
    }

    const url = links().checkoutUrl;
    if (!url) return fail(503, { error: 'Subscriptions are not available right now.' });

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
