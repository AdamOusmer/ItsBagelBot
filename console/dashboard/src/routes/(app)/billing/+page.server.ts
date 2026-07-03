import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import { billingState, checkoutBasketCreate, type BillingState } from '$lib/server/services';
import { RpcError } from '@bagel/shared/server/nats';
import { containsLink } from '@bagel/shared/validation';
import { env } from '$env/dynamic/private';

type BillingLinks = {
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
        source: 'tebex',
        subscriptionRef: 'tbx-r-demo',
        cancelPending: false
      } as BillingState,
      links: {
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
        : ({ active: false, status: 'free', expiresAt: null, source: '', subscriptionRef: null, cancelPending: false } as BillingState),
    links: links(),
    degraded: accountResult.status !== 'fulfilled',
    autostart
  };
};

export const actions: Actions = {
  // Mint a Tebex basket for this user (transactions service -> Headless API)
  // and redirect the browser to Tebex-hosted checkout. The basket URL is still
  // required because it carries our custom user_id for webhook attribution; do
  // not fall back to a static package URL that could charge without attributing
  // the resulting entitlement.
  subscribe: async ({ locals, request, getClientAddress }) => {
    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    if (s.delegate_of || s.impersonator_id) {
      return fail(403, { error: 'Only the account owner can subscribe.' });
    }

    // 'monthly' = auto-renewing subscription, anything else = one paid month.
    // Recurring billing only ever happens on an explicit monthly choice.
    const form = await request.formData();
    const packageType = form.get('plan') === 'monthly' ? 'subscription' : 'single';

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

    let checkoutUrl: string | null = null;
    try {
      const basket = await checkoutBasketCreate(s.user_id, s.login, undefined, getClientAddress(), packageType);
      checkoutUrl = optionalHttpsURL(basket.checkoutUrl ?? undefined);
    } catch (err) {
      console.error('[billing] basket create failed:', err);
    }

    if (!checkoutUrl) return fail(503, { error: 'Subscriptions are not available right now.' });
    throw redirect(303, checkoutUrl);
  },

  // Gift premium to another registered user. The transactions service resolves
  // the Twitch login and vets the recipient (registered, not banned, not
  // already premium); its error strings are user-facing, so surface them
  // verbatim on the gift form. The buyer's own plan does not gate gifting.
  gift: async ({ locals, request, getClientAddress }) => {
    const s = locals.session;
    if (!s) return fail(401, { gift: true, error: 'Not signed in.' });
    if (s.delegate_of || s.impersonator_id) {
      return fail(403, { gift: true, error: 'Only the account owner can gift premium.' });
    }

    const form = await request.formData();
    const recipient = String(form.get('recipient') ?? '').trim();
    // Optional personal note. Capped here as defence-in-depth (the textarea caps
    // it client-side and the transactions service caps + sanitizes again); empty
    // falls back to the default gift email copy. Echoed back on failures so the
    // gift modal can repopulate after a plain-form re-render.
    const message = String(form.get('message') ?? '').trim().slice(0, 280);

    if (!recipient) return fail(400, { gift: true, error: 'Enter the Twitch username to gift to.', recipient, message });
    if (!/^@?[A-Za-z0-9_]{3,25}$/.test(recipient)) {
      return fail(400, { gift: true, error: 'That does not look like a Twitch username.', recipient, message });
    }
    // No links in the gift note: it is emailed to the recipient, so a link (or
    // any obfuscated form) is refused here as well as in the transactions
    // service. Mirrors @bagel/shared/validation used live on the client.
    if (message && containsLink(message)) {
      return fail(400, { gift: true, error: "Gift notes can't contain links or web addresses. Please remove it and try again.", recipient, message });
    }

    let checkoutUrl: string | null = null;
    try {
      const basket = await checkoutBasketCreate(s.user_id, s.login, recipient, getClientAddress(), undefined, message);
      checkoutUrl = optionalHttpsURL(basket.checkoutUrl ?? undefined);
    } catch (err) {
      if (err instanceof RpcError) {
        console.warn(`[billing] gift rejected for ${s.user_id} -> ${recipient}: ${err.message}`);
        return fail(409, { gift: true, error: err.message, recipient, message });
      }
      console.error('[billing] gift basket create failed:', err);
    }

    if (!checkoutUrl) return fail(502, { gift: true, error: 'Gifting is not available right now. Try again in a moment.', recipient, message });
    throw redirect(303, checkoutUrl);
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
