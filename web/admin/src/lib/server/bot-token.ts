// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';

// The caller verifies Twitch bot identity and scopes before reaching this dedicated RPC.
export async function botTokenSet(userId: string, accessToken: string, refreshToken: string): Promise<void> {
  const prefix = process.env.NATS_ADMIN_USER_SUBJECT_PREFIX || 'bagel.rpc.admin.user';
  await rpc(`${prefix}.bot_token_set`, {
    actor_id: userId,
    user_id: userId,
    access_token: accessToken,
    refresh_token: refreshToken
  }, 5000);
}
