// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type {
  GiveawayAlertWire,
  GiveawayDetailWire,
  GiveawayPreviewWire,
  GiveawayWinnerWire,
  GiveawayWire
} from '#lib/server/giveaways';
import {
  demoGiveawayDetail,
  demoGiveawayDraftPreview,
  demoGiveawayHistory,
  demoGiveawayPreview
} from '../giveaway-fixtures';

export * from '#lib/server/giveaways';

type PreviewInput = { campaignId: string } | { winnerCount: number; prizeMonths: number };

export async function giveawaysList(): Promise<GiveawayWire[]> {
  return demoGiveawayHistory(null).giveaways;
}

export async function giveawayGet(input: { campaignId: string }): Promise<GiveawayDetailWire> {
  return demoGiveawayDetail(input.campaignId, null).giveaway;
}

export async function giveawayPreview(input: PreviewInput): Promise<GiveawayPreviewWire> {
  return 'campaignId' in input ? demoGiveawayPreview() : demoGiveawayDraftPreview(input.winnerCount, input.prizeMonths);
}

export async function giveawayInclusion(input: { userId: string; include: boolean }): Promise<GiveawayPreviewWire> {
  return demoGiveawayPreview(input);
}

export async function giveawayCreate(input: { title: string; winnerCount: number; prizeMonths: number }): Promise<GiveawayWire> {
  const draft = demoGiveawayHistory(null).giveaways[0];
  return { ...draft, ...input, id: `demo-draft-${Date.now()}`, status: 'draft' };
}

export async function giveawayFreeze(): Promise<void> {}

export async function giveawayDraw(): Promise<{ awards: GiveawayWinnerWire[] }> {
  return { awards: [] };
}

export async function giveawayRetry(): Promise<void> {}

export async function giveawayAlerts(): Promise<GiveawayAlertWire[]> {
  return demoGiveawayDetail('', 'drawn').alerts;
}

export async function giveawayHistory(): Promise<GiveawayWinnerWire[]> {
  return [];
}
