// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { giveawaySummary } from '@bagel/kit/giveaway';
import type { GiveawayDetailWire, GiveawayPreviewWire, GiveawayWire } from './giveaways';
import type { BotCounter } from './services';

if (!dev) throw new Error('ADMIN_DEV_FIXTURE_INCLUDED_IN_PRODUCTION');

const campaign = {
  title: 'September community giveaway',
  reason: 'Launch celebration',
  winnerCount: 2,
  prizeMonths: 3,
  createdAt: '2026-09-01T12:00:00Z',
  drawnAt: '2026-09-03T12:00:00Z',
  version: 2
};
const eligible = { eligible: 42, free: 25, premium: 9, subscribers: 8, excluded: 17 };
const exclusions = { banned: 0, inactive: 6, not_onboarded: 2, test_account: 4, vip: 3, current_staff: 2 };
const capabilities = {
  newAwardsEnabled: true,
  schedulingEnabled: false,
  providerMutationsEnabled: false,
  intervalRuleVerified: false,
  reason: 'demo pending protection'
};
const drawnAwards = {
  drawnAt: campaign.drawnAt,
  pendingAwards: 1,
  winners: [
    {
      id: 'award-1', userId: '1001', login: 'bagel_friend', selectedAt: campaign.drawnAt,
      prizeMonths: 3, awardState: 'completed', billingState: 'not_required',
      startAt: '2026-09-03T12:00:00Z', endAt: '2026-12-03T12:00:00Z',
      paidThroughAt: null, nextChargeAt: null, providerVerifiedAt: null, emailState: 'accepted',
      emailWarning: null, failureReason: null, retryCount: 0
    },
    {
      id: 'award-2', userId: '1002', login: 'monthly_winner', selectedAt: campaign.drawnAt,
      prizeMonths: 3, awardState: 'needs_review', billingState: 'pending', startAt: null, endAt: null,
      paidThroughAt: null, nextChargeAt: null, providerVerifiedAt: null, emailState: 'missing_contact',
      emailWarning: 'No contact email', failureReason: 'Provider pause not verified', retryCount: 1
    }
  ],
  alerts: [
    {
      id: 'alert-1', awardId: 'award-2', reason: 'Provider pause not verified',
      pendingSince: campaign.drawnAt, upcomingChargeAt: '2026-09-20T12:00:00Z', urgent: true
    }
  ]
} satisfies Pick<GiveawayDetailWire, 'drawnAt' | 'pendingAwards' | 'winners' | 'alerts'>;

function previewStatus(value: string | null): 'draft' | 'frozen' | null {
  return value === 'draft' || value === 'frozen' ? value : null;
}

export function demoGiveawayHistory(statusParam: string | null): { giveaways: GiveawayWire[]; degraded: false } {
  return {
    giveaways: [{
      ...campaign,
      id: 'demo-september',
      status: previewStatus(statusParam) ?? 'complete',
      summary: giveawaySummary(campaign.winnerCount, campaign.prizeMonths),
      pendingAwards: 0
    }],
    degraded: false
  };
}

export function demoGiveawayDetail(id: string, statusParam: string | null) {
  const status = previewStatus(statusParam) ?? 'drawn';
  const giveaway: GiveawayDetailWire = {
    ...campaign,
    id,
    status,
    eligible,
    selectionMethod: 'random_draw',
    algorithmVersion: 'crypto-rand-partial-fisher-yates-v1',
    poolDigest: 'demo',
    capabilities,
    ...(status === 'drawn' ? drawnAwards : { drawnAt: null, pendingAwards: 0, winners: [], alerts: [] })
  };
  return { giveaway, degraded: false, alertsDegraded: false };
}

export function demoGiveawayDraftPreview(winnerCount: number, prizeMonths: number) {
  return { eligible, exclusions, summary: giveawaySummary(winnerCount, prizeMonths), capabilities };
}

export function demoGiveawayPreview(): GiveawayPreviewWire {
  return {
    ...demoGiveawayDraftPreview(campaign.winnerCount, campaign.prizeMonths),
    excluded: [
      { userId: '2001', login: 'paused_streamer', reason: 'inactive', included: true, includable: true },
      { userId: '2002', login: 'staff_tester', reason: 'current_staff', included: false, includable: true },
      { userId: '2003', login: 'banned_account', reason: 'banned', included: false, includable: false }
    ],
    included: 1,
    poolDigest: 'demo-preview'
  };
}

export const demoBotCounters: BotCounter[] = [{ name: 'feeds', scope: 'bot', value: '12873' }];
