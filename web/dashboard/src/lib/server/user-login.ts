// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import { SUB } from './services';

const LOOKUP_TIMEOUT_MS = 4000;

type AccountAgeReply = { target_id?: string; login?: string; user_found?: boolean; error?: string };

export async function currentLogin(userId: string): Promise<string | null> {
  try {
    const reply = await rpc<AccountAgeReply>(`${SUB.outgressRpc}.accountage.get`, { target_id: userId }, LOOKUP_TIMEOUT_MS);
    const login = (reply.login ?? '').trim().toLowerCase();
    return !reply.error && reply.user_found === true && reply.target_id === userId && login ? login : null;
  } catch {
    return null;
  }
}

type Named = { type: string; config: Record<string, string | number | boolean> };

const userIdOf = (node: Named) => (node.type === 'cond.user' && typeof node.config.userId === 'string' ? node.config.userId : '');

export async function attachLogins(nodes: readonly Named[], lookup: (userId: string) => Promise<string | null> = currentLogin): Promise<void> {
  await Promise.all(
    nodes.map(async (node) => {
      const userId = userIdOf(node);
      const login = userId ? await lookup(userId) : null;
      if (login) node.config.login = login;
    })
  );
}
