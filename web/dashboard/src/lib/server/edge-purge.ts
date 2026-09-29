// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { commandsHref } from '@bagel/kit/site-links';
import { SEO_ORIGIN } from './seo-hosts';
import { accountState } from './services';

const DEMO = dev && env.DEMO === '1';

const PURGE_TIMEOUT_MS = 3000;

const PURGE_DEBOUNCE_MS = 1500;

const LOGIN_RE = /^[a-z0-9_]{1,25}$/;

const pending = new Set<string>();

export async function purgeEdge(urls: string[]): Promise<boolean> {
  if (DEMO) return true;

  const zoneId = env.CF_ZONE_ID;
  const token = env.CF_CACHE_PURGE_TOKEN;
  if (!zoneId || !token) return false;

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), PURGE_TIMEOUT_MS);
  try {
    const res = await fetch(`https://api.cloudflare.com/client/v4/zones/${zoneId}/purge_cache`, {
      method: 'POST',
      headers: {
        authorization: `Bearer ${token}`,
        'content-type': 'application/json'
      },
      body: JSON.stringify({ files: urls }),
      signal: controller.signal
    });
    return res.ok;
  } catch {
    return false;
  } finally {
    clearTimeout(timeout);
  }
}

export function channelPageUrls(login: string): string[] {
  return [
    commandsHref(login),
    commandsHref(`@${login}`),
    `${SEO_ORIGIN.leaderboard}/${login}`,
    `${SEO_ORIGIN.leaderboard}/@${login}`
  ];
}

async function purgeChannel(userId: string): Promise<void> {
  const account = await accountState(userId).catch(() => null);
  const login = (account?.username ?? '').toLowerCase();
  if (LOGIN_RE.test(login)) await purgeEdge(channelPageUrls(login));
}

export function schedulePurgeChannel(userId: string): void {
  if (DEMO || pending.has(userId)) return;
  pending.add(userId);
  setTimeout(() => {
    pending.delete(userId);
    void purgeChannel(userId);
  }, PURGE_DEBOUNCE_MS).unref();
}
