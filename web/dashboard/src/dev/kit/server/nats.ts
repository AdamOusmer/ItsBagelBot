// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpcRefusal } from '@bagel/kit/server/nats';
import { answer } from '../../backend';

export * from '@bagel/kit/server/nats';

export function warm(): void {}

export async function ready(): Promise<boolean> {
  return true;
}

export async function rpcReply<T>(subject: string, payload: unknown = {}): Promise<T> {
  return structuredClone(answer(subject, payload)) as T;
}

export async function rpc<T>(subject: string, payload: unknown = {}): Promise<T> {
  const reply = await rpcReply<T>(subject, payload);
  const refusal = rpcRefusal(reply);
  if (refusal) throw refusal;
  return reply;
}

export async function publish(): Promise<void> {}

export function subscribe(): void {}

export function subscribeDurable(): void {}
