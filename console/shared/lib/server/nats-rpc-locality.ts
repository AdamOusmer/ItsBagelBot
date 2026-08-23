// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { RequestError } from '@nats-io/transport-node';

const RPC_NODE_TOKEN = 'node';

export function rpcSubjectsForNode(subject: string, node: string | undefined): string[] {
  if (!node || !/^[^.*>\s]+$/.test(node)) return [subject];
  return [`${subject}.${RPC_NODE_TOKEN}.${node}`, subject];
}

// v3 clients drop the ErrorCode enum: requests reject with a RequestError whose
// cause is a NoRespondersError exactly when no local responder exists.
function isNoResponders(error: unknown): boolean {
  return error instanceof RequestError && error.isNoResponders();
}

// A generic retry is safe only when NATS proves no local responder exists.
export async function requestLocalFirst<T>(
  subjects: string[],
  request: (subject: string) => Promise<T>
): Promise<T> {
  for (let i = 0; i < subjects.length; i++) {
    try {
      return await request(subjects[i]);
    } catch (error) {
      if (i === subjects.length - 1 || !isNoResponders(error)) throw error;
    }
  }
  throw new Error('no responders');
}
