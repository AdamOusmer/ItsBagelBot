// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { FeedHistory } from '#lib/server/feed-history';
import { demoFeedEvent } from '../fixtures';

export * from '#lib/server/feed';

const INTERVAL_MS = 3_000;
const HISTORY_SIZE = 40;
let history: FeedHistory | undefined;

export async function statusHistory(prefix: string): Promise<FeedHistory> {
  if (history) return history;
  const feed = new FeedHistory();
  const now = Date.now();
  let sequence = 0;
  for (; sequence < HISTORY_SIZE; sequence++) {
    feed.add(demoFeedEvent(sequence, prefix, new Date(now - (HISTORY_SIZE - sequence) * INTERVAL_MS).toISOString()));
  }
  setInterval(() => feed.add(demoFeedEvent(sequence++, prefix, new Date().toISOString())), INTERVAL_MS).unref();
  history = feed;
  return feed;
}
