// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ChannelLifecycle } from '@bagel/kit/connection-state';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { rpc } from '@bagel/kit/server/nats';
import { defineRead, defineWrite, READ_TIMEOUT_MS, WRITE_TIMEOUT_MS } from '@bagel/kit/server/service';
import { PUBLIC_PAGES, pageHiddenField, pageSetVerb, type PageHiddenField, type PublicPage } from '#lib/public-pages';
import { SUB } from './subjects';
import { broadcastInvalidation, fabric, invalidate } from './fabric';

export type AccountStatus = 'free' | 'paid' | 'vip';
export type MarketingStatus = 'none' | 'granted' | 'withdrawn';
export type AccountState = {
  lifecycleRevision?: number;
  accountCreatedAt?: number;
  lifecycle?: ChannelLifecycle;
  active: boolean;
  status: AccountStatus;
  onboarded: boolean;
  termsVersion?: string;
  marketingStatus: MarketingStatus;
  creatorCode: string | null;
  username: string;
  displayName: string;
};

export function normalizeStatus(raw: string | undefined): AccountStatus {
  const s = (raw ?? 'free').toLowerCase();
  return s === 'paid' || s === 'vip' ? (s as AccountStatus) : 'free';
}

export type StateReply = {
  active?: boolean;
  status?: string;
  onboarded?: boolean;
  terms_version?: string;
  marketing_status?: MarketingStatus;
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
} & { [K in PageHiddenField]?: unknown };

const stateRead = {
  subject: `${SUB.dashboard}.state_get`,
  request: (userId: string) => ({ broadcaster_user_id: userId }),
  map: (r: StateReply) => r,
  timeoutMs: READ_TIMEOUT_MS
};

const boardState = defineRead({
  ...stateRead,
  cache: {
    fabric,
    key: (userId: string) => `state:${userId}`,
    policy: POLICY.entity
  }
});

export function stateView<T>(view: (r: StateReply) => T): (userId: string) => Promise<T> {
  return async (userId) => view(await boardState(userId));
}

function toAccountState(r: StateReply): AccountState {
  return {
    lifecycle: r.lifecycle,
    lifecycleRevision: r.state_revision,
    accountCreatedAt: r.account_created_at,
    active: !!r.active,
    status: normalizeStatus(r.status),
    onboarded: !!r.onboarded,
    termsVersion: r.terms_version,
    marketingStatus: r.marketing_status ?? 'none',
    creatorCode: r.creator_code?.trim() ? r.creator_code : null,
    username: (r.username ?? '').trim(),
    displayName: (r.display_name ?? '').trim()
  };
}

export const accountState = stateView(toAccountState);
export const freshAccountState = defineRead({ ...stateRead, map: toAccountState });

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

function publicPageToggle(page: PublicPage) {
  const field = pageHiddenField(page);
  return {
    read: stateView((r) => r[field] !== true),
    write: stateWrite<boolean>({ verb: pageSetVerb(page), field })
  };
}

export const pageToggles = Object.fromEntries(PUBLIC_PAGES.map((page) => [page, publicPageToggle(page)])) as Record<PublicPage, ReturnType<typeof publicPageToggle>>;

export const { read: userCommandsPage, write: setCommandsPage } = pageToggles.commands;
export const { read: userLeaderboardPage, write: setLeaderboardPage } = pageToggles.leaderboard;
export const { read: userSongQueuePage, write: setSongQueuePage } = pageToggles.songqueue;

type LifecycleRetryRequest = { broadcaster_user_id: string };
export async function retryLifecycle(request: LifecycleRetryRequest): Promise<unknown> {
  const result = await rpc(`${SUB.dashboard}.lifecycle_retry`, request, WRITE_TIMEOUT_MS);
  await broadcastInvalidation({ scope: 'status', id: request.broadcaster_user_id });
  return result;
}
