// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Public (unauthenticated) per-channel boards for /stats.
//
// Two leaderboards sit under the fleet odometer, both read-only:
//
//   traffic — the channels that moved the most chat and events, from the
//             per-channel `messages_processed` / `events_processed` counters
//             sesame writes beside the fleet-wide pair (see bot_stats.go)
//   feed    — "feed the bagel", one bagel fed by every channel, from the
//             modules service's permanent per-channel rows
//
// Unlike the odometer next to them, these move slowly and cost more per read
// (two ranked counter queries, a board query, and one name lookup per listed
// channel), so a fresh read is rare. The caching is deliberately two-layer:
//
//   Valkey (BOARDS_TTL_MS) — ONE snapshot for the whole deployment. The fabric
//     cache is in-process, so a pod-local window alone let three pods answer
//     three different rankings; a reader hopping pods between two frames saw
//     the board flicker between them. The shared key makes every pod serve the
//     same bytes for the same window.
//   fabric  (POLICY.board)  — a short in-process window in front of it, so a
//     busy pod reads Valkey a few times a second at most, not once per request.
//
// The same three rules as public-stats.ts apply: one shared snapshot for every
// visitor, an absent counter is honestly 0, and an unreachable service degrades
// to an empty board with `degraded: true` instead of erroring the render.
import { rpc } from '@bagel/shared/server/nats';
import { dev } from '$app/environment';
import { POLICY } from '@bagel/shared/server/cache-keys';
import { sharedSnapshot } from '@bagel/shared/server/shared-snapshot';
import { fabric, SUB, accountState } from './services';

// Gated on the build-time `dev` constant first, so Rollup erases the demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && process.env.DEMO === '1';

const CACHE_KEY = 'public-stats:boards';

// The cross-pod snapshot. Versioned because the shape is stored, not derived:
// a payload change must not be read back by an older pod mid-rollout.
const SHARED_KEY = 'public-stats:boards:v1';

// How long one snapshot serves the whole deployment. Deliberately the stream's
// own tick: the boards ride the same 2s frames as the odometer, so a feeding or
// a rank change lands as fast as the message counters beside it. Because the
// key is shared, that cadence costs the fleet one board read every 2s in total
// — not one per pod, and never one per viewer.
const BOARDS_TTL_MS = 2_000;

const COUNTER_MESSAGES = 'messages_processed';
const COUNTER_EVENTS = 'events_processed';

const RPC_TIMEOUT_MS = 4000;

/** Rows shown on the page. */
export const BOARD_SIZE = 10;

// Read deeper than we show, then merge: the two counter boards are ranked
// independently, and a channel can sit in the top of one and just outside the
// top of the other. Reading 3x the shown size makes a missing cell rare without
// turning the query into a scan.
const BOARD_FETCH = BOARD_SIZE * 3;

export interface ChannelTraffic {
  id: string;
  /** Twitch login, or '' when the users service could not name the channel. */
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
  /** Fleet-wide feedings, the number of ranked channels, and the podium. */
  feed: { total: number; ranked: number; entries: FeedEntry[] };
  /** True when any half could not be read; the half itself is then empty. */
  degraded: boolean;
}

interface CounterRankWire {
  user_id?: string;
  value?: number;
}

interface CounterBoardWire {
  board?: CounterRankWire[];
  error?: string;
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

/**
 * One counter's cross-channel ranking as an id -> value map.
 *
 * Returns null when loyalty could not answer, which degrades the whole traffic
 * board: half a board is a wrong board, since the ranking is the point.
 */
async function counterBoard(name: string): Promise<Map<string, number> | null> {
  try {
    const reply = await rpc<CounterBoardWire>(
      `${SUB.loyalty}.counter.board`,
      { user_id: '0', name, limit: BOARD_FETCH },
      RPC_TIMEOUT_MS
    );
    const ranked = new Map<string, number>();
    for (const row of reply.board ?? []) {
      const id = (row.user_id ?? '').trim();
      if (id) ranked.set(id, count(row.value));
    }
    return ranked;
  } catch {
    return null;
  }
}

/**
 * Name one channel for the board.
 *
 * The users service is the only authority on a channel's login (never a
 * caller-supplied string), and its answer is cached per channel for minutes, so
 * a board refresh usually resolves every row without an RPC. A lookup that
 * fails leaves the row unnamed rather than dropping it — the numbers are still
 * true, and the page prints an "unnamed channel" label.
 */
async function channelName(id: string): Promise<string> {
  try {
    return (await accountState(id)).username;
  } catch {
    return '';
  }
}

/** Merge the two rankings into one row set, ranked by messages. */
function mergeTraffic(messages: Map<string, number>, events: Map<string, number>): ChannelTraffic[] {
  const ids = new Set([...messages.keys(), ...events.keys()]);
  const rows: ChannelTraffic[] = [];
  for (const id of ids) {
    rows.push({ id, name: '', messages: messages.get(id) ?? 0, events: events.get(id) ?? 0 });
  }
  rows.sort((a, b) => b.messages - a.messages || b.events - a.events);
  return rows;
}

/**
 * Cut the merged rows to the shown size and name them. The cut comes first, so
 * a board refresh costs at most BOARD_SIZE name lookups however deep the two
 * counter boards were read.
 */
async function nameTraffic(rows: ChannelTraffic[]): Promise<ChannelTraffic[]> {
  const shown = rows.slice(0, BOARD_SIZE);
  const names = await Promise.all(shown.map((row) => channelName(row.id)));
  return shown.map((row, i) => ({ ...row, name: names[i] }));
}

async function loadTraffic(): Promise<ChannelTraffic[] | null> {
  const [messages, events] = await Promise.all([counterBoard(COUNTER_MESSAGES), counterBoard(COUNTER_EVENTS)]);
  if (messages === null || events === null) return null;
  return nameTraffic(mergeTraffic(messages, events));
}

const EMPTY_FEED = { total: 0, ranked: 0, entries: [] as FeedEntry[] };

/**
 * The feed leaderboard, straight from the modules service. Unlike the traffic
 * board it needs no name lookup: each channel's display name is stored with its
 * row at the moment it fed, so the board can name a channel the users service
 * has never heard a login for.
 */
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

/**
 * The deployment-wide snapshot: one board read per tick for the whole fleet,
 * not one per pod. Without it three pods answer three rankings and a reader
 * hopping pods between frames watches the board flicker between them.
 *
 * A degraded snapshot is never published: it is the empty board, and pinning
 * that across every pod would turn one service blip into a window of blank
 * leaderboards everywhere.
 */
function sharedBoards(): Promise<PublicBoards> {
  return sharedSnapshot({
    key: SHARED_KEY,
    ttlMs: BOARDS_TTL_MS,
    load: loadBoards,
    publish: (boards) => !boards.degraded
  });
}

async function loadBoards(): Promise<PublicBoards> {
  const [channels, feed] = await Promise.all([loadTraffic(), loadFeed()]);
  return {
    channels: channels ?? [],
    feed: feed ?? EMPTY_FEED,
    degraded: channels === null || feed === null
  };
}

/**
 * Both public boards. Never rejects: a total failure resolves to empty boards
 * with `degraded: true`, which the page renders as "no board yet" rather than
 * as an error.
 */
export async function publicBoards(): Promise<PublicBoards> {
  if (DEMO) return (await import('./demo-data')).demoBoards(Date.now());
  try {
    return await fabric.readKey(CACHE_KEY, POLICY.board, sharedBoards);
  } catch {
    return { channels: [], feed: EMPTY_FEED, degraded: true };
  }
}
