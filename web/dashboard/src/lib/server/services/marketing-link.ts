// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineRead, defineWrite } from '@bagel/kit/server/service';
import type { MarketingStatus } from './account-state';
import { SUB } from './subjects';

type LinkReply = { ok: boolean; status: MarketingStatus; masked_email?: string; locale?: string };
type OkReply = { ok: boolean };

export type MarketingLink = { status: MarketingStatus; maskedEmail: string; locale: string };
export type LinkChange = {
  token: string;
  status: 'withdrawn';
  source: 'one_click' | 'preference_page';
};
export type SuppressionReason = 'complaint' | 'hard_bounce';
export type Suppression = { email: string; reason: SuppressionReason };
export type DeliveryEvent = 'delivered' | 'bounced' | 'complained';
export type Delivery = { resend_id: string; event: DeliveryEvent };

function requireOk(reply: OkReply): void {
  if (!reply.ok) throw new Error('Marketing write refused');
}

export const marketingLink = defineRead({
  subject: `${SUB.dashboard}.marketing_token_get`,
  request: (token: string) => ({ token }),
  map: (reply: LinkReply): MarketingLink => ({
    status: reply.status,
    maskedEmail: reply.masked_email ?? '',
    locale: reply.locale ?? ''
  })
});

export const setMarketingLink = defineWrite({
  subject: `${SUB.dashboard}.marketing_token_set`,
  request: (change: LinkChange) => change,
  after: requireOk
});

export const suppressAddress = defineWrite({
  subject: `${SUB.usersMarketing}.suppress`,
  request: (suppression: Suppression) => suppression,
  after: requireOk
});

export const recordDelivery = defineWrite({
  subject: `${SUB.usersMarketing}.delivery`,
  request: (delivery: Delivery) => delivery,
  after: requireOk
});
