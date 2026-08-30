// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Overview "This stream" panel: the projector's per-stream metadata
// (title/game/viewer counts, start/end) read through the projector's
// stream-info RPC verb (app/projector/rpc/streaminfo.go) and cached like
// every other live Overview lane.
//
// This module never throws. A transport failure, a timeout, or the projector
// answering an `error` reply all resolve to degradedStreamMeta() -- the same
// honesty contract the rest of the Overview redesign follows (see
// $lib/overview-live.ts): a failed read must render as "not measured", never
// as a confident zero. A cold projector row (no stream event seen yet for
// this broadcaster) is NOT a failure -- the projector answers that with
// `known: false` and no `error`, so it resolves normally with known left
// false, matching the honesty rule the RPC handler itself documents.
import { rpc } from '@bagel/shared/server/nats';
import { POLICY } from '@bagel/shared/server/cache-keys';
import { fabric } from './services';
import { degradedStreamMeta, type StreamMeta } from '$lib/overview-live';

// Same env-var-with-default convention as every other projector RPC subject
// (see SUB in ./services.ts). stream-info is the sibling of the live verb:
// NATS_BROADCASTER_LIVE_SUBJECT / bagel.rpc.broadcaster.live.get (see
// app/projector/main.go's projectorTopics). Not added to the shared SUB
// object: this is the only caller, so a module-local constant avoids growing
// a shared file for one reader.
const STREAM_INFO_SUBJECT =
  process.env.NATS_BROADCASTER_STREAM_INFO_SUBJECT || 'bagel.rpc.broadcaster.stream_info.get';

const RPC_TIMEOUT_MS = 2000;

interface StreamInfoReplyWire {
  title?: string;
  game_name?: string;
  viewer_count?: number;
  peak_viewers?: number;
  started_at?: string;
  ended_at?: string;
  live?: boolean;
  known?: boolean;
  error?: string;
}

// Go's zero time.Time round-trips through encoding/json as this exact string,
// not as null or an absent field -- StartedAt/EndedAt read this way whenever
// the projector has never set them (see internal/projection's StreamInfo).
const ZERO_TIME = '0001-01-01T00:00:00Z';

function isoOrNull(raw: string | undefined): string | null {
  if (!raw || raw === ZERO_TIME) return null;
  return raw;
}

/** Minutes between two known ISO timestamps, 0 if either is missing or the
 *  pair is out of order (a still-running stream has no EndedAt yet). */
function durationMin(startedAt: string | null, endedAt: string | null): number {
  if (!startedAt || !endedAt) return 0;
  const start = Date.parse(startedAt);
  const end = Date.parse(endedAt);
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return 0;
  return Math.floor((end - start) / 60_000);
}

function fromWire(reply: StreamInfoReplyWire): StreamMeta {
  const startedAt = isoOrNull(reply.started_at);
  const endedAt = isoOrNull(reply.ended_at);
  return {
    live: reply.live ?? false,
    known: reply.known ?? false,
    title: reply.title ?? '',
    gameName: reply.game_name ?? '',
    startedAt,
    endedAt,
    viewers: reply.viewer_count ?? 0,
    peakViewers: reply.peak_viewers ?? 0,
    lastDurationMin: durationMin(startedAt, endedAt),
    ok: true
  };
}

async function loadStreamMeta(uid: string): Promise<StreamMeta> {
  const reply = await rpc<StreamInfoReplyWire>(STREAM_INFO_SUBJECT, { broadcaster_id: uid }, RPC_TIMEOUT_MS);
  return fromWire(reply);
}

/**
 * The Overview "This stream" panel's data for one broadcaster. Cached per uid
 * under POLICY.live (1s fresh / 2s SWR, single-flight) -- the same cadence
 * public-stats.ts uses, so a burst of concurrent page loads costs at most one
 * RPC per second per pod, not one per request.
 *
 * Degrades to degradedStreamMeta() on any failure to reach the projector;
 * never throws.
 */
export async function streamMeta(uid: string): Promise<StreamMeta> {
  try {
    return await fabric.readKey(`stream-meta:${uid}`, POLICY.live, () => loadStreamMeta(uid));
  } catch {
    // RpcError (the projector answered with an `error` field -- only a
    // malformed request triggers that) and every other failure (timeout,
    // transport, JSON parse) both read as "could not measure" here, so there
    // is nothing to branch on.
    return degradedStreamMeta();
  }
}
