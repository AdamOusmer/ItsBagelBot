// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { logger } from '@bagel/kit/server/logger';
import { rpc, rpcRefusal, rpcReply } from '@bagel/kit/server/nats';
import { SUB } from './services/subjects';
import { riotConfigured } from './riot-oauth';

export type RiotAccount = { puuid: string; game_name: string; tag_line: string; shard: string };
type UserId = string;
type ErrorSlug = string;
type RiotLinkStatus = { linked: false } | (RiotAccount & { linked: true; linked_at: number });
type RiotLinkMutation = 'set' | 'clear';
export type RiotConnection = {
  account: string | null;
  shard: string;
  configured: boolean;
  unavailable: boolean;
  errorSlug: ErrorSlug;
};

export function riotLinkStore(uid: UserId) {
  const user = { user_id: uid };
  return {
    status: () => rpc<RiotLinkStatus>(`${SUB.riotLink}.status`, user, 3000),
    set: (account: RiotAccount) => mutateLink('set', { ...account, ...user }),
    clear: () => mutateLink('clear', user)
  };
}

async function mutateLink(action: RiotLinkMutation, payload: { user_id: UserId } & Partial<RiotAccount>): Promise<void> {
  const reply = await rpcReply<{ error?: string }>(`${SUB.riotLink}.${action}`, payload);
  const refusal = rpcRefusal(reply);
  if (refusal) throw refusal;
}

export function riotConnectionDefaults(errorSlug: ErrorSlug): RiotConnection {
  return { account: null, shard: '', configured: riotConfigured(), unavailable: false, errorSlug };
}

export async function readRiotConnection(uid: UserId, defaults: RiotConnection): Promise<RiotConnection> {
  try {
    const status = await riotLinkStore(uid).status();
    if (!status.linked) return defaults;
    return { ...defaults, account: `${status.game_name}#${status.tag_line}`, shard: status.shard };
  } catch (err) {
    logger.warn({ err }, '[riot] link status unavailable');
    return { ...defaults, unavailable: true };
  }
}
