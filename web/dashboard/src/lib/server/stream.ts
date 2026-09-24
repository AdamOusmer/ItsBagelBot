// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { fabric } from './services';
import {
  finiteOrNull,
  allRead,
  degradedStreamMeta,
  type StreamMeta
} from '$lib/overview-live';

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

const GO_ZERO_TIME_JSON = '0001-01-01T00:00:00Z';

function isoOrNull(raw: string | undefined): string | null {
  if (!raw || raw === GO_ZERO_TIME_JSON) return null;
  return raw;
}

function durationMin(startedAt: string | null, endedAt: string | null): number {
  const span = [startedAt, endedAt].map((iso) => (iso ? finiteOrNull(Date.parse(iso)) : null));
  if (!allRead(span)) return 0;
  const [start, end] = span;
  return end > start ? Math.floor((end - start) / 60_000) : 0;
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

export async function streamMeta(uid: string): Promise<StreamMeta> {
  try {
    return await fabric.readKey(`stream-meta:${uid}`, POLICY.live, () => loadStreamMeta(uid));
  } catch {
    return degradedStreamMeta();
  }
}
