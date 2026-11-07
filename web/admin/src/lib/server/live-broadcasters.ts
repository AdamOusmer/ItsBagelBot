// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';

// The live set is read in the projector with pkg/valkey.ScanAll
// (internal/domain/live.BroadcasterIDs). The console does not scan Valkey
// itself: a second client would miss the shared timeouts, TLS, and the rule
// that a scan which stops early is not a complete answer.
const LIVE_LIST_SUBJECT = process.env.NATS_BROADCASTER_LIVE_LIST_SUBJECT || 'bagel.rpc.broadcaster.live.list';
const LIVE_LIST_TIMEOUT_MS = 4_000;

// currentLiveBroadcasterIDs is who is on air right now.
// null means the set could not be read. Callers must not treat that as
// "nobody is live".
export async function currentLiveBroadcasterIDs(): Promise<number[] | null> {
  try {
    const reply = await rpc<{ ids?: number[]; error?: string }>(LIVE_LIST_SUBJECT, {}, LIVE_LIST_TIMEOUT_MS);
    if (reply.error) return null;
    return reply.ids ?? [];
  } catch {
    return null;
  }
}
