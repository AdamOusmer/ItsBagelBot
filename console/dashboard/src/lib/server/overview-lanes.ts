// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The Overview redesign's five live-panel reads, bundled as one lane set so
// the page loader and the /overview/stream SSE endpoint serve byte-identical
// shapes from one place. The lanes are the panels whose data changes while
// the page is open (stream metadata, per-stream counters, chat volume, the
// activity feed and its answered-tonight fold) — none of which publish cache
// invalidations, so the layout's /events stream never fires for them and a
// snapshot-only read would sit frozen until a manual reload. The SSE endpoint
// re-reads these lanes on a short tick instead; everything else on the page
// stays invalidation-driven.
//
// Every read degrades to its own default rather than throwing (the honesty
// contract in $lib/overview-live.ts), so callers may await these without
// rejection arms.
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import {
  degradedAnsweredTonight,
  type StreamMeta,
  type StreamCounters,
  type ChatVolume,
  type ActivityFeed,
  type AnsweredTonight
} from '$lib/overview-live';
import { streamMeta } from '$lib/server/stream';
import { streamCounters } from '$lib/server/stream-counters';
import { chatVolume } from '$lib/server/chat-volume';
import { activityFeed } from '$lib/server/activity';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// demoOr streams either the demo fixture or the real read as an unawaited
// promise so SvelteKit streams it: the page shell flushes immediately and each
// section hydrates when its round trip lands, instead of blocking SSR (and the
// post-login redirect) on NATS. The fixture side is a dynamic import so the
// whole demo-data module drops out of a production build with the branch.
function demoOr<T>(pick: (m: typeof import('$lib/server/demo-data')) => T, real: () => Promise<T>): Promise<T> {
  return DEMO ? import('$lib/server/demo-data').then(pick) : real();
}

// Fold the activity feed into per-command counts, highest first.
//
// SCOPE CAVEAT, and the reason the panel says "recently" rather than naming the
// whole stream: the feed store is hard-capped (50 rows), so these counts cover
// the rows still in that window, not every command answered since the stream
// started. A true per-stream total would need a per-command counter, and the
// counter namespace is system-owned with one registered name per counter — a
// row per broadcaster per trigger is not a schema this can mint.
function answeredFromFeed(feed: ActivityFeed): AnsweredTonight {
  if (!feed.ok) return degradedAnsweredTonight();
  const counts = new Map<string, number>();
  for (const row of feed.rows) {
    if (row.kind !== 'command') continue;
    const trigger = row.text.split(' ')[0];
    if (!trigger) continue;
    counts.set(trigger, (counts.get(trigger) ?? 0) + 1);
  }
  const commands = [...counts.entries()]
    .map(([name, count]) => ({ name, count }))
    .sort((a, b) => b.count - a.count)
    .slice(0, 5);
  return { commands, ok: true };
}

export type OverviewLanes = {
  stream: Promise<StreamMeta>;
  counters: Promise<StreamCounters>;
  volume: Promise<ChatVolume>;
  feed: Promise<ActivityFeed>;
  answered: Promise<AnsweredTonight>;
};

/** The five live-panel reads for one board, each an unawaited promise. */
export function overviewLanes(uid: string): OverviewLanes {
  return {
    stream: demoOr<StreamMeta>((m) => m.demoStreamMeta(Date.now()), () => streamMeta(uid)),
    counters: demoOr<StreamCounters>((m) => m.demoStreamCounters, () => streamCounters(uid)),
    volume: demoOr<ChatVolume>((m) => m.demoChatVolume(), () => chatVolume(uid)),
    feed: demoOr<ActivityFeed>((m) => m.demoActivityFeed(Date.now()), () => activityFeed(uid)),

    // Answered-tonight has no backend of its own: it is the same per-command
    // data the feed already carries, folded by trigger. Deriving it here keeps
    // one source of truth rather than a second store that could disagree with
    // the log rendered beside it.
    answered: demoOr<AnsweredTonight>(
      (m) => m.demoAnsweredTonight,
      () => activityFeed(uid).then(answeredFromFeed)
    )
  };
}
