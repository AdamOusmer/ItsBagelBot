// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import { billingState, checkoutBasketCreate, type BillingState } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { RpcError } from '@bagel/shared/server/nats';
import { logger } from '@bagel/shared/server/logger';
import { containsLink } from '@bagel/shared/validation';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Who a billing action operates on: the owner's account. Runnable by the owner,
// or by a delegate explicitly granted the billing section (Tebex handles the
// actual payment identity at checkout). Admin impersonation (view-as) never
// spends. Returns null when the caller may not act. delegate_of / delegate_login
// carry the OWNER's id + login (set in delegate/enter).
function billingActor(s: Session | null | undefined): { id: string; login: string } | null {
  if (!s || s.impersonator_id) return null;
  if (s.delegate_of) {
    if (!(s.sections ?? []).includes('billing')) return null;
    return { id: s.delegate_of, login: s.delegate_login ?? s.login };
  }
  return { id: s.user_id, login: s.login };
}

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

// 'monthly' or anything else (the "buy one month" button posts 'once'),
// pulled out so the subscribe action's own DEMO branch stays a single line.
function demoSubscribePlan(form: FormData): 'monthly' | 'single' {
  return form.get('plan') === 'monthly' ? 'monthly' : 'single';
}

// One set of recipient rules for both paths, demo and live. Returns plain data
// rather than calling fail() itself: SvelteKit infers each action's ActionData
// from fail() calls written directly inside that action, so fail() is still
// called at the gift action's own call sites below.
type GiftFormError = { gift: true; error: string; recipient: string; message: string };
type GiftFailure = { ok: false; status: number; data: GiftFormError };

function giftValidate(form: FormData): { ok: true; recipient: string; message: string } | GiftFailure {
  const recipient = String(form.get('recipient') ?? '').trim();
  const message = String(form.get('message') ?? '').trim().slice(0, 280);
  if (!recipient) {
    return { ok: false, status: 400, data: { gift: true, error: 'Enter the Twitch username to gift to.', recipient, message } };
  }
  if (!/^@?[A-Za-z0-9_]{3,25}$/.test(recipient)) {
    return { ok: false, status: 400, data: { gift: true, error: 'That does not look like a Twitch username.', recipient, message } };
  }
  if (message && containsLink(message)) {
    return {
      ok: false,
      status: 400,
      data: { gift: true, error: "Gift notes can't contain links or web addresses. Please remove it and try again.", recipient, message }
    };
  }
  return { ok: true, recipient, message };
}

// Never send an already-premium account to Tebex: a staff-granted period, an
// active Tebex entitlement, or a VIP grant must run out before a new charge is
// possible. Returns the refusal to surface, or null when the sale may proceed.
async function premiumAlreadyHeld(
  ownerId: string
): Promise<{ status: number; data: { error: string } } | null> {
  try {
    const state = await billingState(ownerId);
    if (state.status === 'free') return null;
    return {
      status: 409,
      data: {
        error:
          'This account already has premium. Subscribing again is blocked so nobody is double-charged.'
      }
    };
  } catch {
    return { status: 502, data: { error: 'Could not verify the current plan. Try again in a moment.' } };
  }
}

// Entitlement is attributed to the owner; Tebex collects payment from whoever
// completes checkout. Null means no usable URL came back, which the caller
// turns into the one user-facing failure this has.
async function subscribeCheckout(
  actor: { id: string; login: string },
  packageType: 'subscription' | 'single',
  ipAddress: string
): Promise<string | null> {
  try {
    const basket = await checkoutBasketCreate({
      userId: actor.id,
      username: actor.login,
      ipAddress,
      packageType
    });
    return optionalHttpsURL(basket.checkoutUrl ?? undefined);
  } catch (err) {
    logger.error({ err }, '[billing] basket create failed');
    return null;
  }
}

// The basket call and its failure mapping, lifted out of the action. The RPC's
// own error strings are user-facing (the transactions service vets the
// recipient: registered, not banned, not already premium), while anything else
// is ours to log and generalise.
async function giftCheckout(
  session: { user_id: string; login: string },
  recipient: string,
  message: string,
  ipAddress: string
): Promise<{ ok: true; url: string } | GiftFailure> {
  try {
    const basket = await checkoutBasketCreate({
      userId: session.user_id,
      username: session.login,
      recipientUsername: recipient,
      ipAddress,
      giftMessage: message
    });
    const url = optionalHttpsURL(basket.checkoutUrl ?? undefined);
    if (url) return { ok: true, url };
  } catch (err) {
    if (err instanceof RpcError) {
      logger.warn({ err }, `[billing] gift rejected for ${session.user_id} -> ${recipient}`);
      return { ok: false, status: 409, data: { gift: true, error: err.message, recipient, message } };
    }
    logger.error({ err }, '[billing] gift basket create failed');
  }
  return {
    ok: false,
    status: 502,
    data: {
      gift: true,
      error: 'Gifting is not available right now. Try again in a moment.',
      recipient,
      message
    }
  };
}

export const load: PageServerLoad = async ({ locals, url }) => {
  // ?subscribe=1 comes from the marketing site's pricing page (rides through
  // the login flow); the page auto-opens checkout when the plan allows it.
  const autostart = url.searchParams.get('subscribe') === '1';

  if (DEMO) {
    const { demoBilling } = await import('$lib/server/demo-data');
    const { account, links } = demoBilling();
    return { account, links: links satisfies BillingLinks, degraded: false, autostart };
  }

  const s = locals.session;
  if (!s) throw redirect(302, '/login');
  // Billing is owner-only unless a delegate was explicitly granted the billing
  // section (then they manage it on the owner's behalf — see billingActor).
  const isDelegate = !!s.delegate_of;
  if (isDelegate && !(s.sections ?? []).includes('billing')) throw redirect(302, '/');

  // The board being read: the owner's for a delegate, otherwise the user's own.
  const uid = s.delegate_of ?? s.user_id;

  // Returning from hosted checkout lands here (?checkout=complete). The
  // entitlement is applied by an async Tebex webhook, which publishes a `status`
  // invalidation on the cache bus; that both drops the server cache and — via the
  // live SSE stream — re-fetches this page, so the view flips to premium on its
  // own. No special-casing needed in the load.
  const accountResult = await billingState(uid).then(
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
    // Demo stands in for Tebex-hosted checkout with our own fake checkout page,
    // so the whole purchase journey is clickable without a session or an RPC.
    // Guarded on the module-level DEMO const (dev + env.DEMO), same as the load.
    if (DEMO) {
      const plan = demoSubscribePlan(await request.formData());
      throw redirect(303, `/billing/demo-checkout?kind=premium&plan=${plan}`);
    }

    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    const actor = billingActor(s);
    if (!actor) return fail(403, { error: 'You do not have access to manage billing.' });

    // 'monthly' = auto-renewing subscription, anything else = one paid month.
    // Recurring billing only ever happens on an explicit monthly choice.
    const form = await request.formData();
    const packageType = form.get('plan') === 'monthly' ? 'subscription' : 'single';

    const blocked = await premiumAlreadyHeld(actor.id);
    if (blocked) return fail(blocked.status, blocked.data);

    const url = await subscribeCheckout(actor, packageType, getClientAddress());
    if (!url) return fail(503, { error: 'Subscriptions are not available right now.' });
    throw redirect(303, url);
  },

  // Gift premium to another registered user. The transactions service resolves
  // the Twitch login and vets the recipient (registered, not banned, not
  // already premium); its error strings are user-facing, so surface them
  // verbatim on the gift form. The buyer's own plan does not gate gifting.
  gift: async ({ locals, request, getClientAddress }) => {
    // Same validation as the live path (so the gift modal's errors behave
    // identically in demo), then hand off to our own fake checkout instead of
    // minting a Tebex basket.
    if (DEMO) {
      const validated = giftValidate(await request.formData());
      if (!validated.ok) return fail(validated.status, validated.data);
      throw redirect(303, `/billing/demo-checkout?kind=gift&plan=single&recipient=${encodeURIComponent(validated.recipient)}`);
    }

    const s = locals.session;
    if (!s) return fail(401, { gift: true, error: 'Not signed in.' });
    // A gift is the buyer's own purchase (they pay, the recipient gets premium),
    // so the buyer stays the acting session user below — but access is still
    // gated to owners + billing-granted delegates.
    if (!billingActor(s)) return fail(403, { gift: true, error: 'You do not have access to manage billing.' });

    const validated = giftValidate(await request.formData());
    if (!validated.ok) return fail(validated.status, validated.data);

    const checkout = await giftCheckout(s, validated.recipient, validated.message, getClientAddress());
    if (!checkout.ok) return fail(checkout.status, checkout.data);
    throw redirect(303, checkout.url);
  },

  // Cancellation/account management lives on Tebex. We still gate the button
  // behind an owner session so delegated or view-as sessions cannot act on it.
  cancel: async ({ locals }) => {
    // A real cancellation just redirects out to Tebex-hosted management; the
    // demo has nowhere to redirect to, so it flips cancelPending itself and
    // sends the browser straight back.
    if (DEMO) {
      const { demoCancelPending } = await import('$lib/server/demo-data');
      demoCancelPending();
      throw redirect(303, '/billing');
    }

    const s = locals.session;
    if (!s) return fail(401, { error: 'Not signed in.' });
    const actor = billingActor(s);
    if (!actor) return fail(403, { error: 'You do not have access to manage billing.' });

    const url = links().cancelUrl;
    if (!url) return fail(503, { error: 'Subscription management is not available right now.' });

    try {
      const state = await billingState(actor.id);
      if (state.status !== 'paid' || state.source !== 'tebex') {
        return fail(409, { error: 'There is no Tebex subscription to cancel for this account.' });
      }
    } catch {
      return fail(502, { error: 'Could not verify the current plan. Try again in a moment.' });
    }

    throw redirect(303, url);
  }
};
