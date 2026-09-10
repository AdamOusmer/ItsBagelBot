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
import { readModuleBlob } from './module-blob';
import { effectiveId } from './board';
import { MOD, type ModuleDef } from '@bagel/shared';

export { canonicalMinecraftUUID };

// resolveMinecraftUUID turns a linked-account field into a uuid to persist.
// An already-uuid value is canonicalized without an RPC. A username that
// Mojang will not map (or a down gossip hop) returns ''. The 3s budget sits
// in the module-save request path, so it stays well under the action timeout:
// a slow resolve degrades to the username instead of hanging the save.
export async function resolveMinecraftUUID(account: string, isPremium: boolean): Promise<string> {
  const trimmed = account.trim();
  if (!trimmed) return '';
  const typed = canonicalMinecraftUUID(trimmed);
  if (typed) return typed;
  try {
    const r = await rpc<{ uuid?: string }>(
      `${SUB.gossip}.hypixel.uuid`,
      { account: trimmed, is_premium: isPremium },
      3000
    );
    return canonicalMinecraftUUID(r.uuid ?? '') ?? '';
  } catch {
    return '';
  }
}

const MINECRAFT_UUID_MODULES = new Set<string>([MOD.urchin, MOD.mcsr]);

// storedLinkedUUID returns the uuid already persisted for def when the stored
// linked account is the same name (Minecraft names are case-insensitive), so
// a transient resolve failure does not wipe a good binding. A different name,
// no row, or a failed read returns ''.
async function storedLinkedUUID(def: ModuleDef, account: string, locals: App.Locals): Promise<string> {
  try {
    const { configs } = await readModuleBlob<Record<string, unknown>>(effectiveId(locals.session), def.id);
    const prevName = String(configs.account ?? '').trim();
    if (prevName.toLowerCase() !== account.toLowerCase()) return '';
    return canonicalMinecraftUUID(String(configs.accountUuid ?? '')) ?? '';
  } catch {
    return '';
  }
}

// attachLinkedUUID writes accountUuid next to a linked Minecraft username so
// the projector hash sesame reads already has the uuid Hypixel requires and
// Urchin/MCSR accept. A failed resolve keeps the uuid already stored for the
// same name (Mojang being down must not undo a good binding) and otherwise
// leaves the username in place.
export async function attachLinkedUUID(
  def: ModuleDef,
  config: Record<string, string>,
  locals: App.Locals
): Promise<Record<string, string>> {
  if (!MINECRAFT_UUID_MODULES.has(def.id)) return config;
  if (!('account' in config)) {
    // accountUuid is derived, never client-authored: a patch cannot set it
    // without the account it belongs to.
    delete config.accountUuid;
    return config;
  }
  const account = (config.account ?? '').trim();
  config.account = account;
  if (!account) {
    config.accountUuid = '';
    return config;
  }
  config.accountUuid =
    (await resolveMinecraftUUID(account, await broadcasterPremium(locals))) ||
    (await storedLinkedUUID(def, account, locals));
  return config;
}
