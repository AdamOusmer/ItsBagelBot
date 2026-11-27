// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import { env } from '@bagel/kit/server/env';
import { SUB } from './subjects';
import { normalizeStatus, stateView, type AccountStatus } from './account-state';

export type BillingGrantSource = 'tebex' | 'admin' | '';

export type BillingState = {
  active: boolean;
  status: AccountStatus;
  expiresAt: string | null;
  source: BillingGrantSource;
  subscriptionRef: string | null;
  cancelPending: boolean;
  paymentFailed?: boolean;
};

export const billingState = stateView((r): BillingState => ({
  active: !!r.active,
  status: normalizeStatus(r.status),
  expiresAt: r.expires_at ?? null,
  source: (r.source as BillingGrantSource) ?? '',
  subscriptionRef: r.subscription_ref ?? null,
  cancelPending: !!r.subscription_cancel_pending,
  paymentFailed: !!r.subscription_payment_failed
}));

export type CheckoutBasket = { ident: string; checkoutUrl: string | null; recipientLogin: string | null };

export type CheckoutPackageType = 'single' | 'subscription';

export type CheckoutRequest = {
  userId: string;
  username: string;
  recipientUsername?: string;
  ipAddress?: string;
  packageType?: CheckoutPackageType;
  giftMessage?: string;
};

export async function checkoutBasketCreate(req: CheckoutRequest): Promise<CheckoutBasket> {
  const r = await rpc<{ ident?: string; checkout_url?: string; recipient_login?: string }>(
    `${SUB.transactions}.basket_create`,
    {
      user_id: req.userId,
      username: req.username,
      recipient_username: req.recipientUsername || undefined,
      ip_address: req.ipAddress || undefined,
      package_type: req.packageType || undefined,
      gift_message: req.giftMessage || undefined
    },
    16000
  );
  if (!r.ident) throw new Error('basket create returned no ident');
  return { ident: r.ident, checkoutUrl: httpsURL(r.checkout_url), recipientLogin: r.recipient_login ?? null };
}

export type BillingLinks = { cancelUrl: string | null };

export function billingLinks(): BillingLinks {
  return { cancelUrl: httpsURL(env.TEBEX_CANCEL_URL) };
}

function httpsURL(value: string | undefined): string | null {
  if (!value) return null;
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'https:' ? parsed.toString() : null;
  } catch {
    return null;
  }
}
