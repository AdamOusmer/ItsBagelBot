// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import type { DonationsReply } from '#lib/donations-view';

const DONATIONS_SUBJECT = 'rpc.admin.premium.donations';
const READ_TIMEOUT = 5000;

export function donationsSummary(actorId: string, days: number): Promise<DonationsReply> {
  return rpc<DonationsReply>(DONATIONS_SUBJECT, { actor_id: actorId, days }, READ_TIMEOUT);
}
