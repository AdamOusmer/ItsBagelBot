// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { DonationsReply } from '#lib/donations-view';
import { demoDonations } from '../fixtures';

export * from '#lib/server/donations';

export async function donationsSummary(_actorId: string, days: number): Promise<DonationsReply> {
  return demoDonations(days);
}
