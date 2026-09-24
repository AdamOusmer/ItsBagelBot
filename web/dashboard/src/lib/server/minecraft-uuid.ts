// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc } from '@bagel/kit/server/nats';
import { SUB } from './services';
import { canonicalMinecraftUUID } from './minecraft-id';
import { broadcasterPremium } from './module-gate';
import { readModuleBlob } from './module-blob';
import { effectiveId } from './board';
import { MOD, type ModuleDef } from '@bagel/kit';

export { canonicalMinecraftUUID };

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

export async function attachLinkedUUID(
  def: ModuleDef,
  config: Record<string, string>,
  locals: App.Locals
): Promise<Record<string, string>> {
  if (!MINECRAFT_UUID_MODULES.has(def.id)) return config;
  if (!('account' in config)) {
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
