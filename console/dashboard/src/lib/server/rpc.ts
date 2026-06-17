// Dashboard-facing RPC wrappers over the shared NATS client. Subjects come from
// env with the same defaults as the retired Go dashboard tier.
import { rpc, publish } from '@bagel/shared/server/nats';
import type { CommandView, Perm, Tier } from '@bagel/shared';
import { env } from '$env/dynamic/private';

const SUB = {
  broadcaster: env.NATS_BROADCASTER_STATUS_SUBJECT ?? 'bagel.rpc.broadcaster.status.get',
  dashboard: env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard',
  commands: env.NATS_COMMANDS_SUBJECT_PREFIX ?? 'bagel.rpc.commands',
  outgress: env.NATS_OUTGRESS_SYSTEM_SUBJECT ?? 'twitch.outgress.system',
  audit: env.NATS_ADMIN_AUDIT_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user.audit'
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

// isBanned reports whether the platform has banned the user (completes the
// admin "ban from service" action by blocking dashboard login). Fails OPEN:
// an RPC blip returns false so a transient outage never locks everyone out —
// the admin panel remains the source of truth for re-banning.
export async function isBanned(userId: string): Promise<boolean> {
  try {
    const r = await rpc<{ banned?: boolean }>(SUB.broadcaster, { broadcaster_id: userId }, 2000);
    return r.banned === true;
  } catch {
    return false;
  }
}

// auditImpersonation records a dashboard write performed while an admin is
// viewing as the user. Best-effort: a logging failure must never block the
// action it describes, so callers fire-and-forget and we swallow errors here.
export async function auditImpersonation(
  actorId: string,
  actorLogin: string,
  action: string,
  target: string,
  detail: string
): Promise<void> {
  try {
    await rpc(`${SUB.audit}.append`, {
      actor_id: actorId,
      actor_login: actorLogin,
      action,
      target,
      detail,
      ok: true,
      error: ''
    });
  } catch {
    /* best-effort */
  }
}

// Dashboard reads are cached primary-key lookups, so they return in low ms when
// healthy. Cap them at 2s (like tier()) so a slow or missing responder degrades
// the page fast instead of hanging SSR to the 5s default and tripping a gateway 500.
const READ_TIMEOUT_MS = 2000;

export async function hasGrant(userId: string): Promise<boolean> {
  const r = await rpc<{ has_grant: boolean }>(
    `${SUB.dashboard}.grant_has`,
    { broadcaster_user_id: userId },
    READ_TIMEOUT_MS
  );
  return !!r.has_grant;
}

export type AccountStatus = 'free' | 'paid' | 'vip';
export type AccountState = { active: boolean; status: AccountStatus };

function normalizeStatus(raw: string | undefined): AccountStatus {
  const s = (raw ?? 'free').toLowerCase();
  return s === 'paid' || s === 'vip' ? (s as AccountStatus) : 'free';
}

// Receive toggle and billing tier (free/paid/vip) in one round trip. The users
// service loads a single cached user view to answer both, so the page render
// asks once via state_get instead of separate active_get + status_get calls.
export async function accountState(userId: string): Promise<AccountState> {
  const r = await rpc<{ active: boolean; status: string }>(
    `${SUB.dashboard}.state_get`,
    { broadcaster_user_id: userId },
    READ_TIMEOUT_MS
  );
  return { active: !!r.active, status: normalizeStatus(r.status) };
}

export async function setActive(userId: string, active: boolean): Promise<void> {
  await rpc(`${SUB.dashboard}.active_set`, { broadcaster_user_id: userId, active });
}

// Persist the broadcaster's Twitch OAuth grant (the per-channel bot token the
// dashboard consent mints). Called once on login: without it the user row exists
// but the bot has no token to act in the channel.
export async function saveGrant(
  userId: string,
  accessToken: string,
  refreshToken: string
): Promise<void> {
  await rpc(`${SUB.dashboard}.grant_save`, {
    broadcaster_user_id: userId,
    access_token: accessToken,
    refresh_token: refreshToken
  });
}

export async function listCommands(userId: string): Promise<CommandView[]> {
  const r = await rpc<{ commands: CommandView[] }>(`${SUB.commands}.list`, { user_id: userId });
  return r.commands ?? [];
}

export interface CommandInput {
  name: string;
  response: string;
  isActive: boolean;
  perm: Perm;
  cooldown: number;
  allowedUserId: string;
}

// originalName, when set and different from cmd.name, renames the command: the
// commands service updates the existing row's name field in place instead of
// deleting the old command and recreating it under the new name.
export async function upsertCommand(
  userId: string,
  cmd: CommandInput,
  originalName?: string
): Promise<{ commands: CommandView[]; error?: string }> {
  const r = await rpc<{ commands: CommandView[]; error?: string }>(`${SUB.commands}.upsert`, {
    user_id: userId,
    name: cmd.name,
    response: cmd.response,
    is_active: cmd.isActive,
    perm: cmd.perm,
    cooldown: cmd.cooldown,
    allowed_user_id: cmd.allowedUserId,
    original_name: originalName ?? ''
  });
  return { commands: r.commands ?? [], error: r.error };
}

export async function deleteCommand(
  userId: string,
  name: string
): Promise<{ commands: CommandView[]; error?: string }> {
  const r = await rpc<{ commands: CommandView[]; error?: string }>(`${SUB.commands}.delete`, {
    user_id: userId,
    name
  });
  return { commands: r.commands ?? [], error: r.error };
}
