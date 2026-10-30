// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { RpcError } from '@bagel/kit/server/nats';
import { healLogin } from '#lib/server/identity-heal';
import { resolveLogin, type ResolvedChannel } from '#lib/server/services';

const DEFINITIVE_MISS: ReadonlySet<string> = new Set(['not_found', 'invalid']);

export type LoginLookup = ResolvedChannel | 'missing' | 'failed';

async function storedLogin(login: string): Promise<LoginLookup> {
  try {
    const found = await resolveLogin(login);
    return found?.userId ? found : 'missing';
  } catch (err) {
    return err instanceof RpcError && DEFINITIVE_MISS.has(err.code) ? 'missing' : 'failed';
  }
}

// Busy or errored heals are 'failed' so the 404 is not edge-cached as a definitive miss.
async function healed(login: string): Promise<LoginLookup> {
  const healing = await healLogin(login);
  if (healing.kind === 'found') return healing.value;
  return healing.kind === 'missing' ? 'missing' : 'failed';
}

export async function lookupLogin(login: string): Promise<LoginLookup> {
  const stored = await storedLogin(login);
  return stored === 'missing' ? healed(login) : stored;
}
