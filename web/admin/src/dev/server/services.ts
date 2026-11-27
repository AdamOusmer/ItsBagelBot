// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { RpcError } from '@bagel/kit/server/nats';
import type { ShardSnapshot, UserStats } from '@bagel/kit';
import {
  AUDIT_MAX_PAGES,
  AUDIT_PAGE_SIZE,
  type AdminAcct,
  type AdminCheck,
  type AdminRole,
  type AdminUserWire,
  type AuditEntry,
  type AuditPage,
  type BotCounter,
  type ChannelSubState,
  type EnrollmentWire,
  type FeedBoard,
  type NotificationPage,
  type ServiceHealth,
  type TokenStatus,
  type TrialMutationReply,
  type TrialSnapshot,
  type UserRef
} from '#lib/server/services';
import {
  demoAdminIdentity,
  demoAuditPageEntries,
  demoEnrollment,
  demoStaff,
  demoStaffHistory,
  sampleAudit,
  sampleFeedBoard,
  sampleHealth,
  sampleNotifications,
  sampleSnapshot,
  sampleStats,
  sampleTrials,
  sampleUsers
} from '../fixtures';
import { demoBotCounters } from '../giveaway-fixtures';

export * from '#lib/server/services';

const shards: ShardSnapshot = structuredClone(sampleSnapshot);
const trials: TrialSnapshot = structuredClone(sampleTrials);
let staff: AdminAcct[] = demoStaff();
const counters = new Map<string, BotCounter>(demoBotCounters.map((c) => [c.name, { ...c }]));

export async function adminCheck(): Promise<AdminCheck> {
  const admin = demoAdminIdentity();
  return { admin: true, role: admin.role, login: admin.login, display_name: admin.display_name };
}

export async function userStats(): Promise<UserStats> {
  return sampleStats;
}

export async function userEnrollment(_actorId: string, days: number): Promise<EnrollmentWire> {
  return demoEnrollment(days);
}

export async function shardSnapshot(): Promise<ShardSnapshot> {
  return structuredClone(shards);
}

export async function shardScale(count: number): Promise<ShardSnapshot> {
  shards.desired_count = count;
  return shardSnapshot();
}

export async function shardAutoscale(enabled: boolean): Promise<ShardSnapshot> {
  shards.autoscale = enabled;
  return shardSnapshot();
}

export async function trialList(): Promise<TrialSnapshot> {
  return structuredClone(trials);
}

export async function trialAdd(broadcasterId: string): Promise<TrialMutationReply> {
  trials.trials.push({ broadcaster_id: broadcasterId, enabled: true, state: 'pending' });
  return { broadcaster_id: broadcasterId, state: 'pending', enabled: true };
}

export async function trialRemove(broadcasterId: string): Promise<TrialMutationReply> {
  trials.trials = trials.trials.filter((t) => t.broadcaster_id !== broadcasterId);
  return { broadcaster_id: broadcasterId, state: 'removed' };
}

export async function trialSetEnabled(broadcasterId: string, enabled: boolean): Promise<TrialMutationReply> {
  const trial = trials.trials.find((t) => t.broadcaster_id === broadcasterId);
  if (trial) trial.enabled = enabled;
  return { broadcaster_id: broadcasterId, state: enabled ? 'receiving' : 'disabled', enabled };
}

export async function serviceHealth(): Promise<ServiceHealth[]> {
  return sampleHealth;
}

export async function feedBoard(): Promise<FeedBoard> {
  return sampleFeedBoard;
}

export async function auditList(): Promise<AuditEntry[]> {
  return sampleAudit;
}

export async function auditPage(page = 1, search = '', actorFilter = ''): Promise<AuditPage> {
  const q = search.toLowerCase();
  const source = actorFilter ? demoStaffHistory(Number(actorFilter)) : demoAuditPageEntries();
  const fields = (e: AuditEntry) => [e.actor_login, e.action, String(e.actor_id), e.target, e.detail, e.error];
  const filtered = q ? source.filter((e) => fields(e).some((f) => f?.toLowerCase().includes(q))) : source;
  const start = (page - 1) * AUDIT_PAGE_SIZE;
  const end = start + AUDIT_PAGE_SIZE;
  return {
    entries: filtered.slice(start, end),
    page,
    page_size: AUDIT_PAGE_SIZE,
    max_pages: AUDIT_MAX_PAGES,
    has_more: end < Math.min(filtered.length, AUDIT_PAGE_SIZE * AUDIT_MAX_PAGES)
  };
}

export async function notificationsList(page = 1): Promise<NotificationPage> {
  return { notifications: page === 1 ? sampleNotifications : [], page, page_size: 20, max_pages: 1, has_more: false };
}

export async function adminListAccts(): Promise<AdminAcct[]> {
  return structuredClone(staff);
}

export async function staffUpsert(
  _actor: { id: string },
  target: { userId: string; login: string; displayName: string; role: AdminRole }
): Promise<AdminAcct[]> {
  const row: AdminAcct = {
    id: Number(target.userId),
    login: target.login,
    display_name: target.displayName,
    role: target.role,
    active: true,
    added_by: 0,
    created_at: new Date().toISOString()
  };
  staff = [...staff.filter((s) => s.id !== row.id), row];
  return adminListAccts();
}

export async function staffRemove(_actor: { id: string }, userId: string): Promise<AdminAcct[]> {
  staff = staff.filter((s) => String(s.id) !== userId);
  return adminListAccts();
}

export async function botCounterList(): Promise<BotCounter[]> {
  return [...counters.values()];
}

export async function botCounterCreate(name: string): Promise<BotCounter> {
  const counter: BotCounter = { name, scope: 'bot', value: '0' };
  counters.set(name, counter);
  return counter;
}

export async function botCounterSet(name: string, value: string): Promise<void> {
  const counter = counters.get(name);
  if (counter) counter.value = value;
}

export async function botCounterDelete(name: string): Promise<void> {
  counters.delete(name);
}

function sampleUser(ref: UserRef | string): AdminUserWire {
  const key = typeof ref === 'string' ? ref : ref.userId;
  const user = sampleUsers.find((u) => String(u.id) === key || u.username === key);
  if (!user) throw new RpcError('user not found', 'not_found');
  return user;
}

const patchUser = (ref: UserRef, patch: Partial<AdminUserWire>): AdminUserWire => Object.assign(sampleUser(ref), patch);

export async function userLookup(_actorId: string, q: string): Promise<AdminUserWire> {
  return sampleUser(q);
}

export async function tokenStatus(ref: UserRef): Promise<TokenStatus> {
  return { present: sampleUser(ref).status !== 'free' };
}

export async function channelSubState(): Promise<ChannelSubState> {
  return { state: 'ok', error: '', checkedAt: new Date().toISOString() };
}

export async function userSetStatus(ref: UserRef, status: string, expiresAt?: string): Promise<AdminUserWire> {
  return patchUser(ref, { status, subscription_expires_at: expiresAt ?? undefined });
}

export async function userSetActive(ref: UserRef, active: boolean): Promise<AdminUserWire> {
  return patchUser(ref, { is_active: active });
}

export async function userSetTestAccount(ref: UserRef, value: boolean): Promise<AdminUserWire> {
  return patchUser(ref, { test_account: value });
}

export async function userSetAlpha(ref: UserRef, value: boolean): Promise<AdminUserWire> {
  return patchUser(ref, { alpha: value });
}

export async function userSetCreatorCode(ref: UserRef, code: string): Promise<AdminUserWire> {
  return patchUser(ref, { creator_code: code || null });
}

export async function userSetVoiceUsage(ref: UserRef): Promise<AdminUserWire> {
  return sampleUser(ref);
}

export async function userBan(ref: UserRef): Promise<AdminUserWire> {
  return patchUser(ref, { banned: true });
}

export async function userUnban(ref: UserRef): Promise<AdminUserWire> {
  return patchUser(ref, { banned: false });
}

export async function userReset(ref: UserRef): Promise<AdminUserWire> {
  return sampleUser(ref);
}

export async function userDelete(): Promise<void> {}

export async function tokenClear(): Promise<void> {}

export async function restartUserEventSub(): Promise<void> {}

export async function publishUserEventSub(): Promise<void> {}

export async function premiumGrantEmail(): Promise<'sent'> {
  return 'sent';
}
