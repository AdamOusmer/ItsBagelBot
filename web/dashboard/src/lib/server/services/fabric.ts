// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import newrelic from 'newrelic';
import { publish } from '@bagel/kit/server/nats';
import { createCacheFabric } from '@bagel/kit/server/cache-fabric';
import type { ScopeMap } from '@bagel/kit/server/invalidation';
import { getServerConfig } from '@bagel/kit/server/config';
import { LOGIN_RE } from '#lib/public-login';
import * as liveHub from '../live-hub';
import { dashboardL1CacheCapacity } from '../config-sanity';

export function userPrefixes(id: string): string[] {
  return [`grant:${id}`, `state:${id}`, `tier:${id}`, `commands:${id}`, `modules:${id}`, `delegations:${id}`, `govee-devices:${id}`, `fetches:${id}`];
}

const stateKeys = (id: string) => [`state:${id}`];

function renamedLogins(keys: readonly string[]): string[] {
  return keys.map((k) => k.toLowerCase()).filter((k) => LOGIN_RE.test(k));
}

const renamedKeys = (id: string, logins: readonly string[]) => [...stateKeys(id), ...renamedLogins(logins).map((l) => `login:${l}`)];

// Scope names must match the Go publishers (internal/domain/invalidate callers); an unlisted scope wipes the whole board.
export const SCOPES: ScopeMap = {
  grant: (id) => [`grant:${id}`, `state:${id}`],
  status: (id) => [`state:${id}`, `tier:${id}`, `ban:${id}`],
  commands: (id) => [`commands:${id}`],
  modules: (id) => [`modules:${id}`],
  delegation: (id) => [`delegations:${id}`],
  notifications: (id) => [id === '*' ? 'notifications:' : `notifications:${id}`],
  locale: stateKeys,
  cursor: stateKeys,
  commands_page: stateKeys,
  leaderboard_page: stateKeys,
  live: (id) => [`stream-meta:${id}`],
  fetches: (id) => [`fetches:${id}`],
  rename: renamedKeys,
  'token-warm': () => [],
  outgress: () => [],
  loyalty: () => [],
  '*': (id) => [...userPrefixes(id), `ban:${id}`]
};

const SILENT_SCOPES: ReadonlySet<string> = new Set(['live', 'fetches']);

type RenameHandler = (userId: string, logins: string[]) => void;

const renameHandlers: RenameHandler[] = [];

// Registered at boot (not imported here) so edge purging, which reads services, stays out of the fabric's import cycle.
export function onRename(handler: RenameHandler): void {
  renameHandlers.push(handler);
}

function announceRename({ id, keys }: { id: string; keys: readonly string[] }): void {
  const logins = renamedLogins(keys);
  if (logins.length) for (const handler of renameHandlers) handler(id, logins);
}

export const fabric = createCacheFabric({
  app: 'dashboard',
  scopes: SCOPES,
  capacity: dashboardL1CacheCapacity(process.env),
  onInvalidation: (event) => {
    if (event.scope === 'rename') announceRename(event);
    if (event.prefixes.length && !SILENT_SCOPES.has(event.scope)) liveHub.publish(event.id, event.scope);
  }
});

export function invalidate(...prefixes: string[]) {
  fabric.invalidate(...prefixes);
}

export type BoardChange = { scope: string; id: string };

// For writes no backend service announces: evicts here, then tells the other dashboard pods.
export async function broadcastInvalidation(change: BoardChange): Promise<void> {
  invalidate(...(SCOPES[change.scope] ?? SCOPES['*']!)(change.id, []));
  try {
    await publish(`${getServerConfig().cacheInvalidationPrefix}.${change.scope}`, { broadcaster_id: change.id }, { flush: true });
  } catch (err) {
    newrelic.noticeError(err instanceof Error ? err : new Error(String(err)), { component: 'cache-broadcast', scope: change.scope });
  }
}

// A rename leaves the old login's lookup and the account snapshot stale; drops both on this pod.
export function evictRenamed(userId: string, logins: readonly string[]): void {
  invalidate(...renamedKeys(userId, logins));
}

export function invalidateUser(userId: string) {
  invalidate(...userPrefixes(userId));
}

export function startInvalidationListener(): void {
  fabric.start();
}
