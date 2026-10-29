// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { RpcError } from '@bagel/kit/server/nats';
import { resolveLogin, type ResolvedChannel } from '#lib/server/services';

const DEFINITIVE_MISS: ReadonlySet<string> = new Set(['not_found', 'invalid']);

export type LoginLookup = ResolvedChannel | 'missing' | 'failed';

export async function lookupLogin(login: string): Promise<LoginLookup> {
  try {
    const found = await resolveLogin(login);
    return found?.userId ? found : 'missing';
  } catch (err) {
    return err instanceof RpcError && DEFINITIVE_MISS.has(err.code) ? 'missing' : 'failed';
  }
}
