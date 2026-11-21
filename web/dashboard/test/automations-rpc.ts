// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { mock } from 'bun:test';
import { join } from 'node:path';

export type RpcCall = { subject: string; body: Record<string, unknown> };

export interface AutomationsRpcFake {
  calls: RpcCall[];
  replies: Record<string, unknown>;
  moduleEnabled: boolean;
  moduleWrites: boolean[];
  reset(): void;
}

const SERVER = join(import.meta.dir, '../src/lib/server');

export async function installAutomationsRpcFake(): Promise<AutomationsRpcFake> {
  let evict = () => {};
  const fake: AutomationsRpcFake = {
    calls: [],
    replies: {},
    moduleEnabled: true,
    moduleWrites: [],
    reset() {
      fake.calls.length = 0;
      fake.replies = {};
      fake.moduleEnabled = true;
      fake.moduleWrites.length = 0;
      evict();
    }
  };
  const nats = await import('@bagel/kit/server/nats');
  mock.module('@bagel/kit/server/nats', () => ({
    ...nats,
    rpc: async (subject: string, body: Record<string, unknown>) => {
      fake.calls.push({ subject, body });
      const reply = fake.replies[subject];
      if (reply instanceof Error) throw reply;
      if (reply === undefined) throw new Error(`Unexpected RPC: ${subject}`);
      return reply;
    }
  }));
  mock.module(join(SERVER, 'module-blob.ts'), () => ({
    readModuleBlob: async () => ({ enabled: fake.moduleEnabled, configs: {} }),
    setModuleEnabled: async (_uid: string, _mod: string, enabled: boolean) => {
      fake.moduleWrites.push(enabled);
      return undefined;
    }
  }));
  const { fabric } = await import('../src/lib/server/services');
  evict = () => fabric.cache.invalidate('automations:7');
  return fake;
}

export function formRequest(url: string, fields: Record<string, string>): Request {
  return new Request(url, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams(fields).toString()
  });
}
