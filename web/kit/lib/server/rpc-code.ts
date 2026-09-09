// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/**
 * The refusal vocabulary every Go RPC reply now carries in its `code` field,
 * and the one place the console reads it.
 *
 * Before this the console branched on the English sentence in `error`
 * ("already linked to another Twitch channel"), which is unlocalisable and
 * breaks the moment anyone rewords a Go log line -- it did, twice. The Go
 * side is internal/domain/rpc/code.go; rpc-code.test.ts asserts this list and
 * that one are the same, so a rename fails a test rather than turning into a
 * page that silently stops recognising a refusal.
 *
 * Reading a code the reader does not know is treated as no code at all, which
 * is why a caller passes its OWN known set: a page that has no branch for
 * `conflict` must not be handed one it will drop on the floor. RPC_CODES is
 * the default set for callers that handle the whole vocabulary.
 */
export const RPC_CODES = [
  'invalid',
  'not_found',
  'forbidden',
  'conflict',
  'unavailable',
  'internal'
] as const;

export type RpcCode = (typeof RPC_CODES)[number];

/** A reply as this module reads it: an unset field is an absent refusal. */
export type CodedReply = { code?: string; error?: string };

/**
 * The text matches this module replaces, kept only so a console deployed
 * ahead of the services that emit codes still recognises the refusals that
 * change what a page renders.
 *
 * Added 2026-09-08, DELETE AFTER 2026-10-08: by then every db service in the
 * cluster answers with a code, and keeping a substring match alive past that
 * re-creates the bug class this whole change exists to remove. A fallback
 * only fires when the caller's own known set contains the code it maps to.
 */
const LEGACY_TEXT: ReadonlyArray<readonly [string, string]> = [
  ['already linked to another Twitch channel', 'bound_elsewhere'],
  ['invalid user_id', 'invalid'],
  ['bad request', 'invalid']
];

/**
 * Builds the reader for one caller's vocabulary. A factory rather than a
 * function taking the set at every call site: the set is a property of the
 * page, not of the reply, and closing over it keeps the per-call spelling to
 * `replyCode(reply)`.
 */
export function codeReader<C extends string>(known: readonly C[]): (reply: CodedReply) => C | '' {
  const set: ReadonlySet<string> = new Set(known);
  return (reply) => {
    const code = (reply.code ?? '').trim();
    if (set.has(code)) return code as C;
    return legacyCode(reply.error ?? '', set) as C | '';
  };
}

/** legacyCode is the pre-`code` fallback; see LEGACY_TEXT for its expiry. */
function legacyCode(error: string, known: ReadonlySet<string>): string {
  if (error === '') return '';
  const hit = LEGACY_TEXT.find(([text, code]) => known.has(code) && error.includes(text));
  return hit ? hit[1] : '';
}

/** The reader for a caller that handles the whole shared vocabulary. */
export const rpcCode = codeReader(RPC_CODES);
