// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { I18n } from '@bagel/kit/i18n/context';

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
