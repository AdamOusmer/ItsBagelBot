// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { env } from '@bagel/kit/server/env';
import type { BillingLinks, BillingState, CheckoutBasket, CheckoutRequest } from '#lib/server/services/billing';
import { demoBilling } from '../../fixtures';

export * from '#lib/server/services/billing';

export function billingLinks(): BillingLinks {
  return { cancelUrl: new URL('/billing', env.ORIGIN).toString() };
}

export async function billingState(): Promise<BillingState> {
  return demoBilling().account;
}

function checkoutUrl(req: CheckoutRequest): string {
  const url = new URL('/billing/demo-checkout', env.ORIGIN);
  const gift = !!req.recipientUsername;
  url.searchParams.set('kind', gift ? 'gift' : 'premium');
  url.searchParams.set('plan', req.packageType === 'subscription' ? 'monthly' : 'single');
  if (gift) url.searchParams.set('recipient', req.recipientUsername ?? '');
  return url.toString();
}

export async function checkoutBasketCreate(req: CheckoutRequest): Promise<CheckoutBasket> {
  return { ident: 'demo-basket', checkoutUrl: checkoutUrl(req), recipientLogin: req.recipientUsername ?? null };
}
