// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import { dev } from '$app/environment';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { sharedSnapshot } from '@bagel/kit/server/shared-snapshot';
import { fabric, SUB, accountState } from './services';
import { liveBoard } from './live-counters';

const DEMO = dev && process.env.DEMO === '1';

const CACHE_KEY = 'public-stats:boards';

// Bump when the payload shape changes: pods mid-rollout share this key.
const SHARED_KEY = 'public-stats:boards:v1';

const BOARDS_TTL_MS = 2_000;

const COUNTER_MESSAGES = 'messages_processed';
const COUNTER_EVENTS = 'events_processed';

const RPC_TIMEOUT_MS = 4000;

export const BOARD_SIZE = 10;

const BOARD_FETCH = BOARD_SIZE * 3;

export interface ChannelTraffic {
  id: string;
  name: string;
  messages: number;
  events: number;
}

export interface FeedEntry {
  id: string;
  name: string;
  count: number;
}

export interface PublicBoards {
  channels: ChannelTraffic[];
  feed: { total: number; ranked: number; entries: FeedEntry[] };
  degraded: boolean;
}

interface FeedBoardWire {
  entries?: { broadcaster_id?: number | string; name?: string; count?: number }[];
  total?: number;
  ranked?: number;
  error?: string;
}

function count(raw: unknown): number {
  return Number.isFinite(raw) ? Number(raw) : 0;
}

async function channelName(id: string): Promise<string> {
  try {
    return (await accountState(id)).username;
  } catch {
    return '';
  }
}

function mergeTraffic(messages: Map<string, number>, events: Map<string, number>): ChannelTraffic[] {
  const ids = new Set([...messages.keys(), ...events.keys()]);
  const rows: ChannelTraffic[] = [];
  for (const id of ids) {
    rows.push({ id, name: '', messages: messages.get(id) ?? 0, events: events.get(id) ?? 0 });
  }
  rows.sort((a, b) => b.messages - a.messages || b.events - a.events);
  return rows;
}

async function nameTraffic(rows: ChannelTraffic[], known: Map<string, string>): Promise<ChannelTraffic[]> {
  const shown = rows.slice(0, BOARD_SIZE);
  const names = await Promise.all(shown.map((row) => known.get(row.id) ?? channelName(row.id)));
  return shown.map((row, i) => ({ ...row, name: names[i] }));
}

function feedNames(feed: PublicBoards['feed'] | null): Map<string, string> {
  const names = new Map<string, string>();
  for (const entry of feed?.entries ?? []) {
    if (entry.id && entry.name) names.set(entry.id, entry.name);
  }
  return names;
}

async function loadTraffic(known: Map<string, string>): Promise<ChannelTraffic[] | null> {
  const [messages, events] = await Promise.all([liveBoard(COUNTER_MESSAGES, BOARD_FETCH), liveBoard(COUNTER_EVENTS, BOARD_FETCH)]);
  if (messages === null || events === null) return null;
  return nameTraffic(mergeTraffic(messages, events), known);
}

const EMPTY_FEED = { total: 0, ranked: 0, entries: [] as FeedEntry[] };

async function loadFeed(): Promise<PublicBoards['feed'] | null> {
  try {
    const reply = await rpc<FeedBoardWire>(
      `${SUB.modules}.personality.feed.board`,
      { limit: BOARD_SIZE },
      RPC_TIMEOUT_MS
    );
    if (reply.error) return null;
    const entries = (reply.entries ?? []).map((row) => ({
      id: String(row.broadcaster_id ?? ''),
      name: (row.name ?? '').trim(),
      count: count(row.count)
    }));
    return { total: count(reply.total), ranked: count(reply.ranked), entries };
  } catch {
    return null;
  }
}

function sharedBoards(): Promise<PublicBoards> {
  return sharedSnapshot({
    key: SHARED_KEY,
    ttlMs: BOARDS_TTL_MS,
    load: loadBoards,
    publish: (boards) => !boards.degraded
  });
}

async function loadBoards(): Promise<PublicBoards> {
  const feed = await loadFeed();
  const channels = await loadTraffic(feedNames(feed));
  return {
    channels: channels ?? [],
    feed: feed ?? EMPTY_FEED,
    degraded: channels === null || feed === null
  };
}

export async function publicBoards(): Promise<PublicBoards> {
  if (DEMO) return (await import('./demo-data')).demoBoards(Date.now());
  try {
    return await fabric.readKey(CACHE_KEY, POLICY.board, sharedBoards);
  } catch {
    return { channels: [], feed: EMPTY_FEED, degraded: true };
  }
}
