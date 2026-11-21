// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';

const LIVE_LIST_SUBJECT = process.env.NATS_BROADCASTER_LIVE_LIST_SUBJECT || 'rpc.broadcaster.live.list';
const LIVE_LIST_TIMEOUT_MS = 4_000;
let inFlight: Promise<number[] | null> | undefined;

async function readLiveBroadcasterIDs(): Promise<number[] | null> {
  try {
    const reply = await rpc<{ ids?: number[]; error?: string }>(LIVE_LIST_SUBJECT, {}, LIVE_LIST_TIMEOUT_MS);
    if (reply.error) return null;
    return reply.ids ?? [];
  } catch {
    return null;
  }
}

export function currentLiveBroadcasterIDs(): Promise<number[] | null> {
  return inFlight ??= readLiveBroadcasterIDs().finally(() => {
    inFlight = undefined;
  });
}
