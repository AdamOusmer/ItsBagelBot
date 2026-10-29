// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { error } from '@sveltejs/kit';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { MOD } from '@bagel/kit';
import { lookupLogin } from '$lib/server/channel-lookup';
import { markEdge, pageOutcome, readVisibility, type EdgeOutcome } from '$lib/server/edge-cache';
import { readModuleBlob } from '$lib/server/module-blob';
import { channelLabel } from '$lib/server/public-directory';
import { readQueue } from '$lib/server/queue-store';
import { LOGIN_RE, normalizeLogin } from '$lib/public-login';
import { snapshotOf, type QueueSnapshot } from '$lib/queue-snapshot';

const DEMO = dev && env.DEMO === '1';

const QUEUE_HOST = 'queue.itsbagelbot.com';

export type QueueRead = { login: string; channelName: string; snapshot: QueueSnapshot; outcome: EdgeOutcome };

function channelNotFound(): never {
  throw error(404, 'Channel not found');
}

function hideChannel(locals: App.Locals): never {
  markEdge(locals, 'hidden');
  return channelNotFound();
}

export function requireQueueLogin(url: URL, segment: string, locals: App.Locals): string {
  if (!dev && url.hostname !== QUEUE_HOST) {
    markEdge(locals, 'missing');
    throw error(404, 'Not found');
  }
  const login = normalizeLogin(segment);
  if (LOGIN_RE.test(login)) return login;
  markEdge(locals, 'missing');
  return channelNotFound();
}

async function requireChannel(login: string, locals: App.Locals) {
  const looked = await lookupLogin(login);
  if (typeof looked === 'object') return { userId: looked.userId, login, channelName: channelLabel(looked, login) };
  if (looked === 'missing') markEdge(locals, 'missing');
  return channelNotFound();
}

async function demoRead(login: string): Promise<QueueRead> {
  const demo = await import('$lib/server/demo-data');
  const now = Date.now();
  return { login, channelName: login, snapshot: snapshotOf(demo.demoQueue(now), now), outcome: 'page' };
}

export async function readQueueChannel(login: string, locals: App.Locals): Promise<QueueRead> {
  if (DEMO) return demoRead(login);
  const channel = await requireChannel(login, locals);
  const [enabled, view] = await Promise.all([
    readVisibility(readModuleBlob(channel.userId, MOD.queue).then((blob) => blob.enabled)),
    readQueue(channel.userId)
  ]);
  if (enabled === false) hideChannel(locals);
  return {
    login: channel.login,
    channelName: channel.channelName,
    snapshot: snapshotOf(view, Date.now()),
    outcome: pageOutcome([enabled], view.degraded)
  };
}
