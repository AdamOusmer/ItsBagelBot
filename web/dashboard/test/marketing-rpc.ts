// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { mock } from 'bun:test';

export const rpcCalls: Array<[subject: string, payload: unknown]> = [];
export const rpcState: { reply: Record<string, unknown>; refusal: string | undefined } = { reply: { ok: true }, refusal: undefined };

export function resetMarketingRpc(): void {
  rpcCalls.length = 0;
  rpcState.reply = { ok: true };
  rpcState.refusal = undefined;
}

export async function installMarketingRpc(): Promise<void> {
  const nats = await import('@bagel/kit/server/nats');
  mock.module('@bagel/kit/server/nats', () => ({
    ...nats,
    rpc: async (subject: string, payload: unknown) => {
      rpcCalls.push([subject, payload]);
      if (rpcState.refusal !== undefined) throw new nats.RpcError('refused', rpcState.refusal as never);
      return rpcState.reply;
    }
  }));
}
