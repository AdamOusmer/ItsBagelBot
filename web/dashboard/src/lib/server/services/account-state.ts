// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ChannelLifecycle } from '@bagel/kit/connection-state';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { defineRead, defineWrite, READ_TIMEOUT_MS } from '@bagel/kit/server/service';
import { SUB } from './subjects';
import { fabric, invalidate } from './fabric';

export type AccountStatus = 'free' | 'paid' | 'vip';
export type AccountState = { lifecycleRevision?: number; accountCreatedAt?: number; lifecycle?: ChannelLifecycle; active: boolean; status: AccountStatus; onboarded: boolean; creatorCode: string | null; username: string; displayName: string };

export function normalizeStatus(raw: string | undefined): AccountStatus {
  const s = (raw ?? 'free').toLowerCase();
  return s === 'paid' || s === 'vip' ? (s as AccountStatus) : 'free';
}

export type StateReply = {
  active?: boolean;
  status?: string;
  onboarded?: boolean;
  creator_code?: string | null;
  username?: string | null;
  display_name?: string | null;
  lifecycle?: ChannelLifecycle;
  state_revision?: number;
  account_created_at?: number;
  expires_at?: string;
  source?: string;
  subscription_ref?: string;
  subscription_cancel_pending?: boolean;
  subscription_payment_failed?: boolean;
  locale?: unknown;
  custom_cursor?: unknown;
  commands_page_hidden?: unknown;
  leaderboard_page_hidden?: unknown;
};

// One state_get reply per board; every view below derives from it, so every state scope must evict `state:`.
const boardState = defineRead({
  subject: `${SUB.dashboard}.state_get`,
  request: (userId: string) => ({ broadcaster_user_id: userId }),
  map: (r: StateReply) => r,
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `state:${userId}`,
    policy: POLICY.entity
  }
});

export function stateView<T>(view: (r: StateReply) => T): (userId: string) => Promise<T> {
  return async (userId) => view(await boardState(userId));
}

export const accountState = stateView((r): AccountState => ({
  lifecycle: r.lifecycle,
  lifecycleRevision: r.state_revision,
  accountCreatedAt: r.account_created_at,
  active: !!r.active,
  status: normalizeStatus(r.status),
  onboarded: !!r.onboarded,
  creatorCode: r.creator_code?.trim() ? r.creator_code : null,
  username: (r.username ?? '').trim(),
  displayName: (r.display_name ?? '').trim()
}));

type StateField = { verb: string; field: string };

function stateWrite<V>({ verb, field }: StateField) {
  return defineWrite<[string, V], unknown>({
    subject: `${SUB.dashboard}.${verb}`,
    request: (userId: string, value: V) => ({ broadcaster_user_id: userId, [field]: value }),
    after: (_result: unknown, userId: string) => invalidate(`state:${userId}`)
  });
}

export const setActive = stateWrite<boolean>({ verb: 'active_set', field: 'active' });
export const setOnboarded = stateWrite<boolean>({ verb: 'onboarded_set', field: 'onboarded' });

export const userLocale = stateView((r) => (typeof r.locale === 'string' && r.locale ? r.locale : 'en'));
export const setLocale = stateWrite<string>({ verb: 'locale_set', field: 'locale' });

export const userCursor = stateView((r) => r.custom_cursor !== false);
export const setCursor = stateWrite<boolean>({ verb: 'cursor_set', field: 'custom_cursor' });

export const userCommandsPage = stateView((r) => r.commands_page_hidden !== true);
export const setCommandsPage = stateWrite<boolean>({ verb: 'commands_page_set', field: 'commands_page_hidden' });

export const userLeaderboardPage = stateView((r) => r.leaderboard_page_hidden !== true);
export const setLeaderboardPage = stateWrite<boolean>({ verb: 'leaderboard_page_set', field: 'leaderboard_page_hidden' });

type LifecycleRetryRequest = { broadcaster_user_id: string };
export const retryLifecycle = defineWrite({
  subject: `${SUB.dashboard}.lifecycle_retry`,
  request: (request: LifecycleRetryRequest) => request,
  after: (_result: unknown, request: LifecycleRetryRequest) => invalidate(`state:${request.broadcaster_user_id}`)
});
