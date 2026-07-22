import { ErrorCode } from 'nats';

const RPC_NODE_TOKEN = 'node';

export function rpcSubjectsForNode(subject: string, node: string | undefined): string[] {
  if (!node || !/^[^.*>\s]+$/.test(node)) return [subject];
  return [`${subject}.${RPC_NODE_TOKEN}.${node}`, subject];
}

function isNoResponders(error: unknown): boolean {
  return (
    typeof error === 'object' &&
    error !== null &&
    'code' in error &&
    (error as { code?: string }).code === ErrorCode.NoResponders
  );
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
