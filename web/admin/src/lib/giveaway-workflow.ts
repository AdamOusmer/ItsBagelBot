// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { I18n } from '@bagel/kit/i18n/context';
import type { GiveawayPreviewWire, GiveawayWire, GiveawayWinnerWire } from './server/giveaways';

type PreviewSnapshot = { poolDigest?: string | null };

export function freezePoolDigest(preview: PreviewSnapshot): string | null {
  return preview.poolDigest?.trim() || null;
}

const EXCLUSION_KEYS = new Map<string, string>([
  ['banned', 'admin.giveaways.reasonBanned'],
  ['inactive', 'admin.giveaways.reasonInactive'],
  ['not_onboarded', 'admin.giveaways.reasonNotOnboarded'],
  ['test_account', 'admin.giveaways.reasonTestAccount'],
  ['vip', 'admin.giveaways.reasonVip'],
  ['current_staff', 'admin.giveaways.reasonCurrentStaff']
]);

export function giveawayExclusionLabel(reason: string, translate: I18n['t']): string {
  const key = EXCLUSION_KEYS.get(reason);
  if (key) return translate(key);
  return reason.replaceAll('_', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase());
}

export const CAMPAIGN_STATUS = {
  all: { label: 'admin.giveaways.statusAll', tone: 'neutral' },
  draft: { label: 'admin.giveaways.statusDraft', tone: 'neutral' },
  frozen: { label: 'admin.giveaways.statusFrozen', tone: 'neutral' },
  drawn: { label: 'admin.giveaways.statusDrawn', tone: 'warning' },
  complete: { label: 'admin.giveaways.statusComplete', tone: 'success' },
  cancelled: { label: 'admin.giveaways.statusCancelled', tone: 'neutral' }
} as const;

export type CampaignFilter = keyof typeof CAMPAIGN_STATUS;

export function campaignCounts(giveaways: ReadonlyArray<Pick<GiveawayWire, 'status'>>): Record<CampaignFilter, number> {
  const counts = Object.fromEntries(Object.keys(CAMPAIGN_STATUS).map((status) => [status, 0])) as Record<CampaignFilter, number>;
  counts.all = giveaways.length;
  for (const { status } of giveaways) counts[status]++;
  return counts;
}

export function giveawayDate(date: string | null | undefined, formatter: Intl.DateTimeFormat, t: I18n['t']): string {
  const timestamp = date ? Date.parse(date) : NaN;
  return Number.isFinite(timestamp) ? formatter.format(timestamp) : t('admin.giveaways.noDate');
}

export function giveawayPrize(campaign: Pick<GiveawayWire, 'winnerCount' | 'prizeMonths'>, number: Intl.NumberFormat, t: I18n['t']): string {
  return t('admin.giveaways.prizePlan', {
    winners: number.format(campaign.winnerCount),
    months: number.format(campaign.prizeMonths)
  });
}

export type GiveawayActionPayload = {
  preview?: GiveawayPreviewWire;
  giveaway?: { id?: string };
  action?: { ok?: boolean; notice?: string };
  error?: string;
};

export function giveawayNotice(result: unknown): { text: string; failed: boolean } | undefined {
  const payload = result as GiveawayActionPayload | null | undefined;
  if (payload?.error) return { text: payload.error, failed: true };
  if (payload?.action?.notice) return { text: payload.action.notice, failed: payload.action.ok === false };
}

export const AWARD_STATUS = {
  selected: { label: 'admin.giveaways.awardSelected', tone: 'neutral' },
  preparing: { label: 'admin.giveaways.awardPreparing', tone: 'neutral' },
  needs_review: { label: 'admin.giveaways.pending', tone: 'warning' },
  scheduled: { label: 'admin.giveaways.awardScheduled', tone: 'neutral' },
  active: { label: 'admin.giveaways.awardActive', tone: 'success' },
  completed: { label: 'admin.giveaways.statusComplete', tone: 'success' },
  voided: { label: 'admin.giveaways.awardVoided', tone: 'danger' }
} as const;

export const BILLING_STATUS = {
  not_required: { label: 'admin.giveaways.notRequired', tone: 'neutral' },
  pending: { label: 'admin.giveaways.pending', tone: 'warning' },
  protected: { label: 'admin.giveaways.protected', tone: 'success' },
  uncertain: { label: 'admin.giveaways.uncertain', tone: 'warning' },
  incident: { label: 'admin.giveaways.incident', tone: 'danger' },
  reconciled: { label: 'admin.giveaways.reconciled', tone: 'success' }
} as const;

export const EMAIL_STATUS = {
  needs_review: { label: 'admin.giveaways.pending', tone: 'warning' },
  queued: { label: 'admin.giveaways.queued', tone: 'neutral' },
  accepted: { label: 'admin.giveaways.emailAccepted', tone: 'success' },
  missing_contact: { label: 'admin.giveaways.emailMissing', tone: 'warning' },
  failed: { label: 'admin.giveaways.failed', tone: 'danger' },
  uncertain: { label: 'admin.giveaways.uncertain', tone: 'warning' }
} as const;

export function canRetryAward(winner: GiveawayWinnerWire): boolean {
  return winner.awardState === 'needs_review' || winner.billingState === 'pending' || winner.billingState === 'uncertain';
}
