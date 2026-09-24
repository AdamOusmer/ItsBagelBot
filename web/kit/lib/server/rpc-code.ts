// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const RPC_CODES = [
  'invalid',
  'not_found',
  'forbidden',
  'conflict',
  'unavailable',
  'internal'
] as const;

export type RpcCode = (typeof RPC_CODES)[number];

export type CodedReply = { code?: string; error?: string };

const LEGACY_TEXT: ReadonlyArray<readonly [string, string]> = [
  ['already linked to another Twitch channel', 'bound_elsewhere'],
  ['invalid user_id', 'invalid'],
  ['bad request', 'invalid']
];

export function codeReader<C extends string>(known: readonly C[]): (reply: CodedReply) => C | '' {
  const set: ReadonlySet<string> = new Set(known);
  return (reply) => {
    const code = (reply.code ?? '').trim();
    if (set.has(code)) return code as C;
    return legacyCode(reply.error ?? '', set) as C | '';
  };
}

function legacyCode(error: string, known: ReadonlySet<string>): string {
  if (error === '') return '';
  const hit = LEGACY_TEXT.find(([text, code]) => known.has(code) && error.includes(text));
  return hit ? hit[1] : '';
}

export const rpcCode = codeReader(RPC_CODES);
