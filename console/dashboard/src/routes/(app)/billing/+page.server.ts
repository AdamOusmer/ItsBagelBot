import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import {
  billingState,
  billingSummary,
  billingCheckout,
  billingCancel,
  type BillingState,
  type BillingSummary
} from '$lib/server/services';
import { env } from '$env/dynamic/private';

const demoSummary: BillingSummary = {
  subscription: {
    reference: 'tbx-r-demo',
    plan_id: 'premium',
    status: 'active',
    started_at: new Date(Date.now() - 45 * 864e5).toISOString(),
    current_period_start: new Date(Date.now() - 15 * 864e5).toISOString(),
    current_period_end: new Date(Date.now() + 15 * 864e5).toISOString()
  },
  payments: [
    {
      id: 'tbx-demo-2',
      status: 'completed',
      amount_cents: 499,
      currency: 'USD',
      created_at: new Date(Date.now() - 15 * 864e5).toISOString()
    },
    {
      id: 'tbx-demo-1',
      status: 'completed',
      amount_cents: 499,
      currency: 'USD',
      created_at: new Date(Date.now() - 45 * 864e5).toISOString()
    }
  ],
  canCheckout: true,
  canCancel: true
};

export const load: PageServerLoad = async ({ locals }) => {
  if (env.DEMO === '1') {
    return {
      account: {
        active: true,
        status: 'paid',
        expiresAt: demoSummary.subscription?.current_period_end ?? null,
        source: 'tebex'
      } as BillingState,
      billing: demoSummary,
      degraded: false
    };
  }

  const s = locals.session;
  // Owner-only: billing is never part of a delegated section grant.
  if (!s || s.delegate_of) throw redirect(302, '/');

  const [accountResult, billingResult] = await Promise.allSettled([
    billingState(s.user_id),
    billingSummary(s.user_id)
  ]);

  return {
    account:
      accountResult.status === 'fulfilled'
        ? accountResult.value
        : ({ active: false, status: 'free', expiresAt: null, source: '' } as BillingState),
    billing:
      billingResult.status === 'fulfilled'
        ? billingResult.value
        : ({ subscription: null, payments: [], canCheckout: false, canCancel: false } as BillingSummary),
    degraded: accountResult.status !== 'fulfilled' || billingResult.status !== 'fulfilled'
  };
};

export const actions: Actions = {
  // Kick off a Tebex checkout and send the browser there. 303 so the POST
  // becomes a GET on the external checkout page.
  subscribe: async ({ locals }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of || s.impersonator_id) {
      return fail(403, { error: 'Only the account owner can subscribe.' });
    }

    // Never send an already-premium user to Tebex: a staff-granted period or a
    // live subscription must run out before a new charge is possible. (The
    // transactions service independently refuses while a Tebex subscription is
    // live; this check also covers admin grants, which live in the users DB.)
    try {
      const state = await billingState(s.user_id);
      if (state.status !== 'free') {
        return fail(409, { error: 'You already have premium. Subscribing again is blocked so you are not double-charged.' });
      }
    } catch {
      return fail(502, { error: 'Could not verify your current plan. Try again in a moment.' });
    }

    let url: string;
    try {
      url = await billingCheckout(s.user_id);
    } catch {
      return fail(502, { error: 'Could not start checkout. Try again in a moment.' });
    }
    throw redirect(303, url);
  },

  cancel: async ({ locals }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of || s.impersonator_id) {
      return fail(403, { error: 'Only the account owner can cancel the subscription.' });
    }

    try {
      await billingCancel(s.user_id);
      return { ok: true, action: 'cancelled' };
    } catch {
      return fail(502, { error: 'Could not cancel. Try again, or contact support.' });
    }
  }
};
