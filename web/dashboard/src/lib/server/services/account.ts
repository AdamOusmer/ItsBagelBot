// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import newrelic from 'newrelic';
import { rpc } from '@bagel/kit/server/nats';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { defineRead, defineWrite, READ_TIMEOUT_MS } from '@bagel/kit/server/service';
import * as valkey from '@bagel/kit/server/valkey-store';
import type { Tier } from '@bagel/kit';
import { SUB } from './subjects';
import { fabric, invalidate, invalidateUser } from './fabric';

// Must match the projector's tierFromStatus.
function tierFromStatus(status: string): Tier {
  const s = status.toLowerCase();
  return s === 'premium' || s === 'vip' || s === 'paid' ? 'premium' : 'standard';
}

export const tier = defineRead({
  subject: SUB.broadcaster,
  request: (broadcasterId: string) => ({ broadcaster_id: broadcasterId }),
  map: (r: { tier: Tier }) => r.tier ?? 'standard',
  timeoutMs: 2000,
  cache: {
    fabric,
    key: (broadcasterId: string) => `tier:${broadcasterId}`,
    policy: POLICY.entity,
    l2: async (broadcasterId: string) => {
      const u = await valkey.getUser(broadcasterId);
      if (!u.known) return { hit: false, value: 'standard' as Tier };
      return { hit: true, value: u.active ? tierFromStatus(u.status) : 'standard' };
    }
  }
});

export async function isBanned(userId: string): Promise<boolean> {
  try {
    return await fabric.readKey(`ban:${userId}`, POLICY.security, async () => {
      const u = await valkey.getUser(userId);
      if (u.known) return u.banned;
      const r = await rpc<{ banned?: boolean }>(SUB.broadcaster, { broadcaster_id: userId }, 2000);
      return r.banned === true;
    });
  } catch (err) {
    newrelic.noticeError(err instanceof Error ? err : new Error(String(err)), {
      component: 'ban-check'
    });
    return false;
  }
}

export const hasGrant = defineRead({
  subject: `${SUB.dashboard}.grant_has`,
  request: (userId: string) => ({ broadcaster_user_id: userId }),
  map: (r: { has_grant: boolean }) => !!r.has_grant,
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `grant:${userId}`,
    policy: POLICY.entity
  }
});

export const saveGrant = defineWrite({
  subject: `${SUB.dashboard}.grant_save`,
  request: (userId: string, accessToken: string, refreshToken: string) => ({
    broadcaster_user_id: userId,
    access_token: accessToken,
    refresh_token: refreshToken
  }),
  after: (_result: unknown, userId: string) => invalidate(`grant:${userId}`, `state:${userId}`)
});

export type ResolvedChannel = { userId: string; username: string; displayName: string };

export const resolveLogin = defineRead({
  subject: `${SUB.dashboard}.login_resolve`,
  request: (login: string) => ({ login }),
  map: (r: { user_id?: string; username?: string; display_name?: string }): ResolvedChannel => ({
    userId: (r.user_id ?? '').trim(),
    username: (r.username ?? '').trim(),
    displayName: (r.display_name ?? '').trim()
  }),
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (login: string) => `login:${login}`,
    policy: POLICY.entity
  }
});

/** Irreversible; the caller must drop the session cookie after it resolves. */
export async function deleteSelf(userId: string): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string }>(`${SUB.dashboard}.delete_self`, {
    user_id: userId
  });
  if (!r.ok) throw new Error(r.error ?? 'delete failed');
  invalidateUser(userId);
}
