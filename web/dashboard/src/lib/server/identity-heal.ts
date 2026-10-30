// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc, RpcError } from '@bagel/kit/server/nats';
import { SUB, syncIdentity, type ResolvedChannel } from './services';
import { createCachedResolver, MISSING_TTL_MS } from './viewer-resolver';

const DEFINITIVE_MISS: ReadonlySet<string> = new Set(['not_found', 'invalid']);

type TwitchIdentity = { id: string; login: string; displayName: string };

type AccountAgeReply = { target_id?: string; login?: string; display_name?: string; user_found?: boolean; error?: string };

// The reply must name the asked login: an id alone could belong to someone who has since renamed away.
function matchingIdentity(reply: AccountAgeReply, login: string): TwitchIdentity | null {
  const id = (reply.target_id ?? '').trim();
  const found = !reply.error && reply.user_found === true;
  if (!found || !id) return null;
  if ((reply.login ?? '').toLowerCase() !== login) return null;
  return { id, login, displayName: (reply.display_name ?? '').trim() || login };
}

async function twitchIdentity(login: string): Promise<TwitchIdentity | null> {
  const reply = await rpc<AccountAgeReply>(`${SUB.outgressRpc}.accountage.get`, { target_login: login }, 4000);
  return matchingIdentity(reply, login);
}

async function registered(twitch: TwitchIdentity): Promise<ResolvedChannel | null> {
  try {
    await syncIdentity(twitch.id, twitch.login, twitch.displayName);
    return { userId: twitch.id, username: twitch.login, displayName: twitch.displayName };
  } catch (err) {
    if (err instanceof RpcError && DEFINITIVE_MISS.has(err.code)) return null;
    throw err;
  }
}

async function heal(login: string): Promise<ResolvedChannel | null> {
  const twitch = await twitchIdentity(login);
  return twitch ? registered(twitch) : null;
}

// A rename the users DB has not seen yet: Twitch names the id, identity_sync moves our row onto the new login.
// Rate-limited and cached like viewer lookups, and a hit lives no longer than a miss so a later rename cannot be masked.
export const healLogin = createCachedResolver<ResolvedChannel>({ lookup: heal, foundTtlMs: MISSING_TTL_MS });
