// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type Redis from 'iovalkey';
import { masterClient } from '@bagel/kit/server/valkey-master';
import { overlayChannel, overlayChannelId, overlayItemFrame, readOverlayMessage } from './overlay-wire';
import { moduleTokenStore, stillValidOverlayHash } from './overlay-token';

const RECHECK_MS = 60_000;
const PING_MS = 25_000;
const MAX_STREAMS_PER_CHANNEL = 8;
const MAX_QUEUED_FRAMES = 16;

const enc = new TextEncoder();

export interface OverlaySink {
  send(frame: string): void;
  close(): void;
}

export interface OverlaySubscriber {
  subscribe(channel: string): Promise<unknown>;
  unsubscribe(channel: string): Promise<unknown>;
  ready(): boolean;
  close(): void;
}

export interface OverlaySubscriberEvents {
  onMessage(channel: string, message: string): void;
  onReady(): void;
}

export interface OverlayHubDeps {
  connect(events: OverlaySubscriberEvents): OverlaySubscriber | null;
  stillValid(channelId: string, tokenHash: string): Promise<boolean>;
}

export interface OverlayHub {
  attach(channelId: string, tokenHash: string, sink: OverlaySink): () => void;
  recheck(): Promise<void>;
  size(): { channels: number; streams: number; connected: boolean };
}

interface Attachment {
  tokenHash: string;
  sink: OverlaySink;
}

function closeSink(sink: OverlaySink): void {
  // One broken stream must never block the rest of the channel.
  try {
    sink.close();
  } catch {}
}

export function createOverlayHub(deps: OverlayHubDeps): OverlayHub {
  const groups = new Map<string, Set<Attachment>>();
  let subscriber: OverlaySubscriber | null = null;
  let recheckTimer: ReturnType<typeof setInterval> | undefined;
  let notReadyTicks = 0;

  function subscribeTo(channelId: string): void {
    subscriber?.subscribe(overlayChannel(channelId)).catch(() => {});
  }

  const events: OverlaySubscriberEvents = {
    onReady() {
      notReadyTicks = 0;
      for (const channelId of groups.keys()) subscribeTo(channelId);
    },
    onMessage(channel, raw) {
      const channelId = overlayChannelId(channel);
      const group = channelId === null ? undefined : groups.get(channelId);
      if (group) deliver(group, raw);
    }
  };

  function deliver(group: Set<Attachment>, raw: string): void {
    const message = readOverlayMessage(raw);
    if (message === null) return;
    if (message === 'revoke') {
      for (const { sink } of [...group]) closeSink(sink);
      return;
    }
    const frame = overlayItemFrame(message);
    for (const { sink } of [...group]) sink.send(frame);
  }

  function rebuildIfStuck(): void {
    if (!subscriber || subscriber.ready()) {
      notReadyTicks = 0;
      return;
    }
    notReadyTicks += 1;
    if (notReadyTicks < 2) return;
    notReadyTicks = 0;
    subscriber.close();
    subscriber = deps.connect(events);
  }

  async function recheckAttachment(channelId: string, attachment: Attachment): Promise<void> {
    // A failed read proves nothing: only a confirmed mismatch ends a live stream.
    const valid = await deps.stillValid(channelId, attachment.tokenHash).catch(() => true);
    if (!valid) closeSink(attachment.sink);
  }

  async function recheck(): Promise<void> {
    rebuildIfStuck();
    const checks: Promise<void>[] = [];
    for (const [channelId, group] of groups) {
      for (const attachment of group) checks.push(recheckAttachment(channelId, attachment));
    }
    await Promise.all(checks);
  }

  function startRunning(): void {
    if (recheckTimer !== undefined) return;
    subscriber = deps.connect(events);
    recheckTimer = setInterval(() => void recheck(), RECHECK_MS);
    recheckTimer.unref?.();
  }

  function stopRunning(): void {
    clearInterval(recheckTimer);
    recheckTimer = undefined;
    subscriber?.close();
    subscriber = null;
  }

  function detach(channelId: string, attachment: Attachment): void {
    const group = groups.get(channelId);
    if (!group?.delete(attachment) || group.size > 0) return;
    groups.delete(channelId);
    subscriber?.unsubscribe(overlayChannel(channelId)).catch(() => {});
    if (groups.size === 0) stopRunning();
  }

  function joinGroup(channelId: string): Set<Attachment> {
    const existing = groups.get(channelId);
    if (existing) return existing;
    const group = new Set<Attachment>();
    groups.set(channelId, group);
    startRunning();
    subscribeTo(channelId);
    return group;
  }

  function attach(channelId: string, tokenHash: string, sink: OverlaySink): () => void {
    const group = joinGroup(channelId);
    const attachment: Attachment = { tokenHash, sink };
    group.add(attachment);
    const [oldest] = group;
    if (group.size > MAX_STREAMS_PER_CHANNEL && oldest) closeSink(oldest.sink);
    return () => detach(channelId, attachment);
  }

  function size() {
    let streams = 0;
    for (const group of groups.values()) streams += group.size;
    return { channels: groups.size, streams, connected: subscriber !== null };
  }

  return { attach, recheck, size };
}

// Same node PUBLISH lands on, so its reply counts these pods. Sentinel kills pubsub clients only when it reconfigures the old primary, about 10s after the switch; failoverDetector moves on +switch-master instead (measured 12s to 2s of lost items).
export function overlaySubscriberOn(base: Redis, events: OverlaySubscriberEvents): OverlaySubscriber {
  const client = base.duplicate({ autoResubscribe: false, failoverDetector: true });
  client.on('error', () => {});
  client.on('ready', events.onReady);
  client.on('message', events.onMessage);
  return {
    subscribe: (channel) => client.subscribe(channel),
    unsubscribe: (channel) => client.unsubscribe(channel),
    ready: () => client.status === 'ready',
    close: () => client.disconnect()
  };
}

function connectValkey(events: OverlaySubscriberEvents): OverlaySubscriber | null {
  const base = masterClient();
  return base ? overlaySubscriberOn(base, events) : null;
}

export const overlayHub = createOverlayHub({
  connect: connectValkey,
  stillValid: (channelId, tokenHash) => stillValidOverlayHash(channelId, tokenHash, moduleTokenStore)
});

export function overlayStreamResponse(
  request: Request,
  hub: OverlayHub,
  channelId: string,
  tokenHash: string
): Response {
  let release = () => {};

  const stream = new ReadableStream<Uint8Array>(
    {
      start(controller) {
        let closed = false;
        let detachSink = () => {};
        let ping: ReturnType<typeof setInterval> | undefined;

        release = () => {
          if (closed) return;
          closed = true;
          clearInterval(ping);
          request.signal.removeEventListener('abort', release);
          detachSink();
        };
        const dropSlowClient = () => {
          release();
          // error(), not close(): close() would keep the queued frames in memory until a dead peer reads them.
          controller.error(new Error('overlay client too slow'));
        };
        const sink: OverlaySink = {
          send(frame) {
            if (closed) return;
            if ((controller.desiredSize ?? 0) <= 0) return dropSlowClient();
            controller.enqueue(enc.encode(frame));
          },
          close() {
            if (closed) return;
            release();
            controller.close();
          }
        };

        sink.send(': connected\n\n');
        sink.send('event: ready\ndata: 1\n\n');
        detachSink = hub.attach(channelId, tokenHash, sink);
        if (closed) detachSink();
        ping = setInterval(() => sink.send(': ping\n\n'), PING_MS);

        if (request.signal.aborted) release();
        else request.signal.addEventListener('abort', release);
      },
      cancel() {
        release();
      }
    },
    new CountQueuingStrategy({ highWaterMark: MAX_QUEUED_FRAMES })
  );

  return new Response(stream, {
    headers: {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-store',
      Connection: 'keep-alive',
      'X-Accel-Buffering': 'no',
      'X-Robots-Tag': 'noindex, nofollow',
      'Referrer-Policy': 'no-referrer',
      'X-Content-Type-Options': 'nosniff'
    }
  });
}
