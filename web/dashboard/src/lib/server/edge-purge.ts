// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { logger } from '@bagel/kit/server/logger';
import { leaderboardCacheTag } from '#lib/edge-tags';
import { accountState } from './services';
import { channelPageUrls } from './channel-urls';

const DEMO = dev && env.DEMO === '1';

const PURGE_TIMEOUT_MS = 3000;

// Cloudflare caps a purge request at 100 URLs or tags.
const PURGE_BATCH = 100;

const NOT_CONFIGURED = -1;

const LOGIN_RE = /^[a-z0-9_]{1,25}$/;

type PurgeKind = { field: 'files' | 'tags'; debounceMs: number; retryMs: number; label: string };

const PAGE_URLS: PurgeKind = { field: 'files', debounceMs: 1500, retryMs: 2000, label: 'channel page' };

// Cloudflare Free allows 5 tag purges per minute (bucket 25): tags batch for 10 s and a 429 waits out one refill.
const LEADERBOARD_TAGS: PurgeKind = { field: 'tags', debounceMs: 10_000, retryMs: 15_000, label: 'leaderboard tag' };

async function purgeRequest(body: Record<string, unknown>): Promise<number> {
  if (DEMO) return 200;

  const zoneId = env.CF_ZONE_ID;
  const token = env.CF_CACHE_PURGE_TOKEN;
  if (!zoneId || !token) return NOT_CONFIGURED;

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), PURGE_TIMEOUT_MS);
  try {
    const res = await fetch(`https://api.cloudflare.com/client/v4/zones/${zoneId}/purge_cache`, {
      method: 'POST',
      headers: {
        authorization: `Bearer ${token}`,
        'content-type': 'application/json'
      },
      body: JSON.stringify(body),
      signal: controller.signal
    });
    return res.status;
  } catch {
    return 0;
  } finally {
    clearTimeout(timeout);
  }
}

function succeeded(status: number): boolean {
  return status >= 200 && status < 300;
}

export async function purgeEdgeByTag(tag: string): Promise<boolean> {
  return succeeded(await purgeRequest({ tags: [tag] }));
}

async function purgeBatchOnce(kind: PurgeKind, body: Record<string, unknown>): Promise<number> {
  const status = await purgeRequest(body);
  if (status !== 429) return status;
  await new Promise((resolve) => setTimeout(resolve, kind.retryMs));
  return purgeRequest(body);
}

async function purgeAll(kind: PurgeKind, items: string[]): Promise<boolean> {
  let ok = true;
  for (let i = 0; i < items.length; i += PURGE_BATCH) {
    const batch = items.slice(i, i + PURGE_BATCH);
    const status = await purgeBatchOnce(kind, { [kind.field]: batch });
    if (status === NOT_CONFIGURED) return false;
    if (succeeded(status)) continue;
    ok = false;
    logger.error({ status, count: batch.length }, `[edge-purge] ${kind.label} purge failed`);
  }
  return ok;
}

class PurgeQueue {
  private readonly pending = new Set<string>();
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(private readonly kind: PurgeKind) {}

  add(items: readonly string[]): void {
    for (const item of items) this.pending.add(item);
    if (this.timer || this.pending.size === 0) return;
    this.timer = setTimeout(() => void this.flush(), this.kind.debounceMs);
    this.timer.unref?.();
  }

  private async flush(): Promise<void> {
    this.timer = null;
    const items = [...this.pending];
    this.pending.clear();
    await purgeAll(this.kind, items);
  }
}

const pageQueue = new PurgeQueue(PAGE_URLS);
const leaderboardQueue = new PurgeQueue(LEADERBOARD_TAGS);

function canonicalLogin(raw: string | null | undefined): string | null {
  const login = (raw ?? '').toLowerCase();
  return LOGIN_RE.test(login) ? login : null;
}

async function knownLogins(userId: string, logins: readonly string[]): Promise<string[]> {
  if (logins.length) return logins.flatMap((l) => canonicalLogin(l) ?? []);
  try {
    const login = canonicalLogin((await accountState(userId)).username);
    return login ? [login] : [];
  } catch (err) {
    logger.error({ err, userId }, '[edge-purge] account lookup failed, purging id pages only');
    return [];
  }
}

export async function purgeChannelPages(userId: string): Promise<boolean> {
  const logins = await knownLogins(userId, []);
  const [pages, boards] = await Promise.all([
    purgeAll(PAGE_URLS, channelPageUrls(userId, logins)),
    logins.length > 0 && purgeAll(LEADERBOARD_TAGS, logins.map(leaderboardCacheTag))
  ]);
  return pages && boards;
}

export function schedulePurgeChannel(userId: string, logins: readonly string[] = []): void {
  if (DEMO) return;
  void knownLogins(userId, logins).then((known) => pageQueue.add(channelPageUrls(userId, known)));
}

// Leaderboard responses are tagged per login: call only on board visibility, loyalty on/off, points reset, rename or deletion.
export function scheduleLeaderboardPurge(userId: string, logins: readonly string[] = []): void {
  if (DEMO) return;
  void knownLogins(userId, logins).then((known) => leaderboardQueue.add(known.map(leaderboardCacheTag)));
}

// Every pod runs this on a rename broadcast: page URLs are cheap, and the debounced queue bounds leaderboard tag purges to one batch per pod per 10 s.
export function purgeRenamedChannel(userId: string, logins: readonly string[]): void {
  schedulePurgeChannel(userId, logins);
  scheduleLeaderboardPurge(userId, logins);
}
