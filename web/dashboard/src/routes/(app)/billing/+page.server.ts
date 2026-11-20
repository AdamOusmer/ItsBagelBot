// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { redirect, fail } from '@sveltejs/kit';
import { billingState, checkoutBasketCreate, type BillingState } from '#lib/server/services';
import type { Session } from '#lib/server/session';
import { RpcError } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { containsLink } from '@bagel/kit/validation';
import { isLoginShape, normalizeLogin } from '#lib/public-login';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { giveawayPrizes, type PrizeAward } from '#lib/server/giveaways';
import { actionError } from '#lib/server/action-errors';
import { count } from '@bagel/kit/server/analytics';

const DEMO = dev && env.DEMO === '1';
const PACKAGE_TYPES = { monthly: 'subscription', single: 'single' } as const;
type CheckoutFailure = 'unavailable' | 'rejected' | 'blocked';
const PREMIUM_HELD_ERRORS = new Set(['that user already has premium', 'that account already has premium coverage']);

function billingActor(s: Session | null | undefined): { id: string; login: string } | null {
  if (!s || s.impersonator_id) return null;
  if (s.delegate_of && !(s.sections ?? []).includes('billing')) return null;
  if (s.delegate_of) {
    return { id: s.delegate_of, login: s.delegate_login ?? s.login };
  }
  return { id: s.user_id, login: s.login };
}

function billingGate(
  s: Session | null | undefined,
  locale: App.Locals['locale']
): { ok: true; actor: { id: string; login: string } } | { ok: false; status: number; error: string } {
  if (!s) return { ok: false, status: 401, error: actionError(locale, 'Not signed in.') };
  const actor = billingActor(s);
  if (!actor) return { ok: false, status: 403, error: actionError(locale, 'You do not have access to manage billing.') };
  return { ok: true, actor };
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

function subscribePlan(form: FormData): 'monthly' | 'single' {
  return form.get('plan') === 'monthly' ? 'monthly' : 'single';
}

type GiftFormError = { gift: true; error: string; recipient: string; message: string };
type GiftFailure = { ok: false; status: number; data: GiftFormError };

function giftFailure(gift: { recipient: string; message: string }, status: number, error: string): GiftFailure {
  return { ok: false, status, data: { gift: true, error, recipient: gift.recipient, message: gift.message } };
}

function giftValidate(form: FormData, locale: App.Locals['locale']): { ok: true; recipient: string; message: string } | GiftFailure {
  const recipient = String(form.get('recipient') ?? '').trim();
  const message = String(form.get('message') ?? '').trim().slice(0, 280);
  if (!recipient) {
    return giftFailure({ recipient, message }, 400, actionError(locale, 'Enter the Twitch username to gift to.'));
  }
  if (!isLoginShape(normalizeLogin(recipient))) {
    return giftFailure({ recipient, message }, 400, actionError(locale, 'That does not look like a Twitch username.'));
  }
  if (message && containsLink(message)) {
    return giftFailure({ recipient, message }, 400, actionError(locale, "Gift notes can't contain links or web addresses. Please remove it and try again."));
  }
  return { ok: true, recipient, message };
}

// Never send an already-premium account to Tebex: the current period must end before a new charge.
async function premiumAlreadyHeld(
  ownerId: string,
  locale: App.Locals['locale']
): Promise<{ status: number; data: { error: string } } | null> {
  try {
    const state = await billingState(ownerId);
    // Must fail closed: checkout during a giveaway reconcile could duplicate Premium coverage.
    const prizes = await giveawayPrizes(ownerId);
    const pendingPrize = prizes.some((prize) => ['selected', 'preparing', 'needs_review', 'scheduled', 'active'].includes(prize.state));
    if (state.status === 'free' && !pendingPrize) return null;
    return {
      status: 409,
      data: {
        error:
          actionError(locale, 'This account already has Premium coverage. Subscribing again is blocked while the current prize or plan is being reconciled.')
      }
    };
  } catch {
    return { status: 502, data: { error: actionError(locale, 'Could not verify the current plan. Try again in a moment.') } };
  }
}

async function tebexSubscriptionMissing(
  ownerId: string,
  locale: App.Locals['locale']
): Promise<{ status: number; data: { error: string } } | null> {
  try {
    const state = await billingState(ownerId);
    if (state.status === 'paid' && state.source === 'tebex') return null;
    return {
      status: 409,
      data: { error: actionError(locale, 'There is no Tebex subscription to cancel for this account.') }
    };
  } catch {
    return { status: 502, data: { error: actionError(locale, 'Could not verify the current plan. Try again in a moment.') } };
  }
}

async function subscribeCheckout(
  actor: { id: string; login: string },
  packageType: 'subscription' | 'single',
  ipAddress: string
): Promise<{ url: string | null; result: CheckoutFailure }> {
  try {
    const basket = await checkoutBasketCreate({
      userId: actor.id,
      username: actor.login,
      ipAddress,
      packageType
    });
    return { url: optionalHttpsURL(basket.checkoutUrl ?? undefined), result: 'unavailable' };
  } catch (err) {
    logger.error({ err }, '[billing] basket create failed');
    return { url: null, result: checkoutError(err) };
  }
}

async function giftCheckout(
  session: { user_id: string; login: string },
  gift: { recipient: string; message: string },
  ipAddress: string,
  locale: App.Locals['locale']
): Promise<{ ok: true; url: string } | (GiftFailure & { result: CheckoutFailure })> {
  const { recipient, message } = gift;
  let result: CheckoutFailure = 'unavailable';
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
    result = checkoutError(err);
    if (err instanceof RpcError) {
      logger.warn({ err }, `[billing] gift rejected for ${session.user_id} -> ${recipient}`);
      return { ...giftFailure(gift, 409, actionError(locale, err.message)), result };
    }
    logger.error({ err }, '[billing] gift basket create failed');
  }
  return { ...giftFailure(gift, 502, actionError(locale, 'Gifting is not available right now. Try again in a moment.')), result };
}

function settledBilling(result: { status: 'fulfilled'; value: BillingState } | { status: 'rejected' }): BillingState {
  return result.status === 'fulfilled'
    ? result.value
    : ({ active: false, status: 'free', expiresAt: null, source: '', subscriptionRef: null, cancelPending: false } as BillingState);
}

function settledPrizes(result: { status: 'fulfilled'; value: PrizeAward[] } | { status: 'rejected' }): PrizeAward[] {
  return result.status === 'fulfilled' ? result.value : [];
}

async function billingPageData(input: { uid: string }): Promise<{ account: BillingState; prizes: PrizeAward[]; degraded: boolean; prizeDegraded: boolean }> {
  const { uid } = input;
  const [accountResult, prizeResult] = await Promise.all([
    billingState(uid).then((value) => ({ status: 'fulfilled' as const, value }), () => ({ status: 'rejected' as const })),
    giveawayPrizes(uid).then((value) => ({ status: 'fulfilled' as const, value }), () => ({ status: 'rejected' as const }))
  ]);
  const account = settledBilling(accountResult);
  const prizes = settledPrizes(prizeResult);
  return {
    account,
    prizes,
    degraded: accountResult.status !== 'fulfilled' || prizeResult.status !== 'fulfilled',
    prizeDegraded: prizeResult.status !== 'fulfilled'
  };
}

function countUpsellClick(url: URL, isDelegate: boolean): void {
  if (isDelegate || url.searchParams.get('from') !== 'overview') return;
  count('upsell.clicks', 'overview');
}

export const load: PageServerLoad = async ({ locals, url }) => {
  const autostart = url.searchParams.get('subscribe') === '1';

  if (DEMO) {
    const { demoBilling } = await import('#lib/server/demo-data');
    const { account, links } = demoBilling();
    return { account, links: links satisfies BillingLinks, degraded: false, prizeDegraded: false, autostart, prizes: [] as PrizeAward[] };
  }

  const s = locals.session;
  if (!s) throw redirect(302, '/login');
  const isDelegate = !!s.delegate_of;
  if (isDelegate && !(s.sections ?? []).includes('billing')) throw redirect(302, '/');

  const uid = s.delegate_of ?? s.user_id;

  countUpsellClick(url, isDelegate);

  const board = await billingPageData({ uid });
  return {
    account: board.account,
    links: links(),
    degraded: board.degraded,
    autostart,
    prizes: board.prizes,
    prizeDegraded: board.prizeDegraded
  };
};

export const actions: Actions = {
  subscribe: async ({ locals, request, getClientAddress }) => {
    const plan = subscribePlan(await request.formData());
    if (DEMO) {
      throw redirect(303, `/billing/demo-checkout?kind=premium&plan=${plan}`);
    }

    const gate = billingGate(locals.session, locals.locale);
    if (!gate.ok) return checkoutFailure(plan, 'rejected', gate.status, { error: gate.error });
    const actor = gate.actor;

    const blocked = await premiumAlreadyHeld(actor.id, locals.locale);
    if (blocked) return checkoutFailure(plan, coverageResult(blocked.status), blocked.status, blocked.data);

    const checkout = await subscribeCheckout(actor, PACKAGE_TYPES[plan], getClientAddress());
    if (!checkout.url) return checkoutFailure(plan, checkout.result, 503, { error: actionError(locals.locale, 'Subscriptions are not available right now.') });
    return checkoutRedirect(plan, checkout.url);
  },

  gift: async ({ locals, request, getClientAddress }) => {
    if (DEMO) {
      const validated = giftValidate(await request.formData(), locals.locale);
      if (!validated.ok) return fail(validated.status, validated.data);
      throw redirect(303, `/billing/demo-checkout?kind=gift&plan=single&recipient=${encodeURIComponent(validated.recipient)}`);
    }

    const gate = billingGate(locals.session, locals.locale);
    if (!gate.ok) return checkoutFailure('gift', 'rejected', gate.status, { gift: true, error: gate.error });
    const s = locals.session!;

    const validated = giftValidate(await request.formData(), locals.locale);
    if (!validated.ok) return checkoutFailure('gift', 'rejected', validated.status, validated.data);

    const checkout = await giftCheckout(s, validated, getClientAddress(), locals.locale);
    if (!checkout.ok) return checkoutFailure('gift', checkout.result, checkout.status, checkout.data);
    return checkoutRedirect('gift', checkout.url);
  },

  cancel: async ({ locals }) => {
    if (DEMO) {
      const { demoCancelPending } = await import('#lib/server/demo-data');
      demoCancelPending();
      throw redirect(303, '/billing');
    }

    const gate = billingGate(locals.session, locals.locale);
    if (!gate.ok) return fail(gate.status, { error: gate.error });
    const actor = gate.actor;

    const url = links().cancelUrl;
    if (!url) return fail(503, { error: actionError(locals.locale, 'Subscription management is not available right now.') });

    const blocked = await tebexSubscriptionMissing(actor.id, locals.locale);
    if (blocked) return fail(blocked.status, blocked.data);

    throw redirect(303, url, { external: true });
  }
};

function checkoutFailure<T>(plan: 'monthly' | 'single' | 'gift', result: CheckoutFailure, status: number, data: T) {
  count('billing.checkout', plan, result);
  return fail(status, data);
}

function checkoutError(err: unknown): CheckoutFailure {
  if (!(err instanceof RpcError)) return 'rejected';
  if (err.code === 'unavailable') return 'unavailable';
  return PREMIUM_HELD_ERRORS.has(err.message) ? 'blocked' : 'rejected';
}

function coverageResult(status: number): CheckoutFailure {
  return status === 409 ? 'blocked' : 'rejected';
}

function checkoutRedirect(plan: 'monthly' | 'single' | 'gift', url: string): never {
  count('billing.checkout', plan, 'redirected');
  throw redirect(303, url, { external: true });
}
