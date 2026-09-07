// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Linked Minecraft accounts: resolve a typed username to a uuid through
// gossip (hypixel.uuid / Mojang) so the projected module config can store it.
// Hypixel requires a uuid; Urchin and MCSR Ranked accept one. A miss (privacy
// block, unknown name, gossip down) leaves the username in place.

import { rpc } from '@bagel/shared/server/nats';
import { SUB } from './services';
import { canonicalMinecraftUUID } from './minecraft-id';
import { broadcasterPremium } from './module-gate';
import { MOD, type ModuleDef } from '@bagel/shared';

export { canonicalMinecraftUUID };

// resolveMinecraftUUID turns a linked-account field into a uuid to persist.
// An already-uuid value is canonicalized without an RPC. A username that
// Mojang will not map (or a down gossip hop) returns ''.
export async function resolveMinecraftUUID(account: string, isPremium: boolean): Promise<string> {
  const trimmed = account.trim();
  if (!trimmed) return '';
  const typed = canonicalMinecraftUUID(trimmed);
  if (typed) return typed;
  try {
    const r = await rpc<{ uuid?: string }>(
      `${SUB.gossip}.hypixel.uuid`,
      { account: trimmed, is_premium: isPremium },
      10000
    );
    return canonicalMinecraftUUID(r.uuid ?? '') ?? '';
  } catch {
    return '';
  }
}

const MINECRAFT_UUID_MODULES = new Set<string>([MOD.urchin, MOD.mcsr]);

// attachLinkedUUID writes accountUuid next to a linked Minecraft username so
// the projector hash sesame reads already has the uuid Hypixel requires and
// Urchin/MCSR accept. A failed resolve leaves the username in place.
export async function attachLinkedUUID(
  def: ModuleDef,
  config: Record<string, string>,
  locals: App.Locals
): Promise<Record<string, string>> {
  if (!MINECRAFT_UUID_MODULES.has(def.id) || !('account' in config)) return config;
  const account = (config.account ?? '').trim();
  config.account = account;
  config.accountUuid = account ? await resolveMinecraftUUID(account, await broadcasterPremium(locals)) : '';
  return config;
}
