// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail } from '@sveltejs/kit';
import type { MutationRefusal } from '@bagel/kit/server/form-action';
import { moduleAction } from './module-action';

export interface RewardFailure {
  missingScope?: boolean;
  missingSpotifyScope?: boolean;
  duplicateTitle?: boolean;
  notAffiliate?: boolean;
  code?: string;
  error?: string;
}

export type RewardOutcome = { ok: true } | ({ ok: false } & RewardFailure);

const FLAG_REFUSALS = [
  ['missingScope', 403, undefined],
  ['missingSpotifyScope', 403, undefined],
  ['duplicateTitle', 409, 'duplicate_title'],
  ['notAffiliate', 403, 'not_affiliate']
] as const;

export function resultFail(r: RewardFailure): MutationRefusal {
  for (const [key, status, error] of FLAG_REFUSALS) {
    if (r[key]) return fail(status, { ok: false, [key]: true, error });
  }
  if (r.code) return fail(400, { ok: false, code: r.code });
  return fail(400, { ok: false, error: r.error ?? 'failed' });
}

export function done(res: RewardOutcome, detail: string): string | MutationRefusal {
  return res.ok ? detail : resultFail(res);
}

export function storeMutator<S>(modId: string, open: (uid: string) => S, auditAs?: string) {
  return (op: string, invalid: string, run: (store: S, f: FormData) => Promise<string | null | MutationRefusal>) =>
    moduleAction(modId, op, (uid, f) => run(open(uid), f), { invalid, auditAs });
}
