// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpcRefusal } from '@bagel/kit/server/nats';

export * from '@bagel/kit/server/nats';

export function warm(): void {}

export async function ready(): Promise<boolean> {
  return true;
}

export async function rpcReply<T>(): Promise<T> {
  return { ok: true } as T;
}

export async function rpc<T>(): Promise<T> {
  const reply = await rpcReply<T>();
  const refusal = rpcRefusal(reply);
  if (refusal) throw refusal;
  return reply;
}

export async function publish(): Promise<void> {}

export function subscribe(): void {}

export function subscribeDurable(): void {}

export async function js(): Promise<never> {
  throw new Error('JetStream is not available under vite dev');
}

export async function jsm(): Promise<never> {
  throw new Error('JetStream is not available under vite dev');
}
