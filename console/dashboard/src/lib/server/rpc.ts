// Dashboard-facing RPC wrappers over the shared NATS client. Subjects come from
// env with the same defaults as the retired Go dashboard tier.
import { rpc, publish } from '@bagel/shared/server/nats';
import type { CommandView, Tier } from '@bagel/shared';
import { env } from '$env/dynamic/private';

const SUB = {
  broadcaster: env.NATS_BROADCASTER_STATUS_SUBJECT ?? 'bagel.rpc.broadcaster.status.get',
  dashboard: env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard',
  commands: env.NATS_COMMANDS_SUBJECT_PREFIX ?? 'bagel.rpc.commands',
  outgress: env.NATS_OUTGRESS_SYSTEM_SUBJECT ?? 'twitch.outgress.system'
};

// Enqueue an EventSub on/off job on the outgress system lane. Outgress runs the
// Helix calls under the shared rate-limit bucket: enabled=true (re)creates the
// channel's EventSub subscriptions, false deletes them.
export async function publishEventSub(broadcasterId: string, enabled: boolean): Promise<void> {
  await publish(SUB.outgress, {
    type: 'eventsub',
    broadcaster_id: broadcasterId,
    payload: { enabled }
  });
}

export async function tier(broadcasterId: string): Promise<Tier> {
  const r = await rpc<{ tier: Tier }>(SUB.broadcaster, { broadcaster_id: broadcasterId }, 2000);
  return r.tier ?? 'standard';
}

export async function hasGrant(userId: string): Promise<boolean> {
  const r = await rpc<{ has_grant: boolean }>(`${SUB.dashboard}.grant_has`, {
    broadcaster_user_id: userId
  });
  return !!r.has_grant;
}

export type AccountStatus = 'free' | 'paid' | 'vip';

// The broadcaster's billing tier (free/paid/vip) from the users service via the
// dashboard-scoped status_get RPC.
export async function accountStatus(userId: string): Promise<AccountStatus> {
  const r = await rpc<{ status: string }>(`${SUB.dashboard}.status_get`, {
    broadcaster_user_id: userId
  });
  const s = (r.status ?? 'free').toLowerCase();
  return s === 'paid' || s === 'vip' ? (s as AccountStatus) : 'free';
}

export async function isActive(userId: string): Promise<boolean> {
  const r = await rpc<{ active: boolean }>(`${SUB.dashboard}.active_get`, {
    broadcaster_user_id: userId
  });
  return !!r.active;
}

export async function setActive(userId: string, active: boolean): Promise<void> {
  await rpc(`${SUB.dashboard}.active_set`, { broadcaster_user_id: userId, active });
}

export async function listCommands(userId: string): Promise<CommandView[]> {
  const r = await rpc<{ commands: CommandView[] }>(`${SUB.commands}.list`, { user_id: userId });
  return r.commands ?? [];
}

export async function upsertCommand(
  userId: string,
  name: string,
  response: string,
  isActive: boolean
): Promise<CommandView[]> {
  const r = await rpc<{ commands: CommandView[] }>(`${SUB.commands}.upsert`, {
    user_id: userId,
    name,
    response,
    is_active: isActive
  });
  return r.commands ?? [];
}

export async function deleteCommand(userId: string, name: string): Promise<CommandView[]> {
  const r = await rpc<{ commands: CommandView[] }>(`${SUB.commands}.delete`, {
    user_id: userId,
    name
  });
  return r.commands ?? [];
}
