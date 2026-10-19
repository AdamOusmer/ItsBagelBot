// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { logger } from '@bagel/kit/server/logger';
import { accountState } from './services';
import { channelPageUrls } from './channel-urls';

const DEMO = dev && env.DEMO === '1';

const PURGE_TIMEOUT_MS = 3000;

const PURGE_DEBOUNCE_MS = 1500;

const RETRY_BACKOFF_MS = 2000;

// Cloudflare caps a single-file purge request at 100 URLs.
const PURGE_BATCH_URLS = 100;

const NOT_CONFIGURED = -1;

const LOGIN_RE = /^[a-z0-9_]{1,25}$/;

const queued = new Set<string>();

let flushTimer: ReturnType<typeof setTimeout> | null = null;

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

async function purgeFilesOnce(urls: string[]): Promise<number> {
  const status = await purgeRequest({ files: urls });
  if (status !== 429) return status;
  await new Promise((resolve) => setTimeout(resolve, RETRY_BACKOFF_MS));
  return purgeRequest({ files: urls });
}

async function purgeFiles(urls: string[]): Promise<boolean> {
  let ok = true;
  for (let i = 0; i < urls.length; i += PURGE_BATCH_URLS) {
    const batch = urls.slice(i, i + PURGE_BATCH_URLS);
    const status = await purgeFilesOnce(batch);
    if (status === NOT_CONFIGURED) return false;
    if (succeeded(status)) continue;
    ok = false;
    logger.error({ status, urls: batch.length }, '[edge-purge] channel page purge failed');
  }
  return ok;
}

function canonicalLogin(raw: string | null | undefined): string | null {
  const login = (raw ?? '').toLowerCase();
  return LOGIN_RE.test(login) ? login : null;
}

async function currentLogins(userId: string): Promise<string[]> {
  try {
    const login = canonicalLogin((await accountState(userId)).username);
    return login ? [login] : [];
  } catch (err) {
    logger.error({ err, userId }, '[edge-purge] account lookup failed, purging id pages only');
    return [];
  }
}

async function channelUrls(userId: string, logins: readonly string[]): Promise<string[]> {
  const known = logins.length ? logins.flatMap((l) => canonicalLogin(l) ?? []) : await currentLogins(userId);
  return channelPageUrls(userId, known);
}

export async function purgeChannelPages(userId: string): Promise<boolean> {
  return purgeFiles(await channelUrls(userId, []));
}

async function flushQueued(): Promise<void> {
  flushTimer = null;
  const urls = [...queued];
  queued.clear();
  await purgeFiles(urls);
}

function enqueue(urls: string[]): void {
  for (const url of urls) queued.add(url);
  flushTimer ??= setTimeout(() => void flushQueued(), PURGE_DEBOUNCE_MS);
  flushTimer.unref?.();
}

export function schedulePurgeChannel(userId: string, logins: readonly string[] = []): void {
  if (DEMO) return;
  void channelUrls(userId, logins).then(enqueue);
}
