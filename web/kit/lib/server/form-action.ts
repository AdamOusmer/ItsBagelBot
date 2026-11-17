// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, type ActionFailure } from '@sveltejs/kit';
import type { ActionOk } from '../action-result';
import { RpcError } from './nats';
import type { RpcCode } from './rpc-code';

export type MutationRefusal = ActionFailure<ActionOk>;

const CLIENT_REFUSAL_STATUS: Partial<Record<RpcCode | '', number>> = {
  invalid: 400,
  forbidden: 403,
  not_found: 404,
  conflict: 409
};

export function thrownStatus(err: unknown): number {
  const code = err instanceof RpcError ? err.code : '';
  return CLIENT_REFUSAL_STATUS[code] ?? 502;
}

export type MutationSpec<Ctx> = {
  gate: () => Ctx | null | Promise<Ctx | null>;
  refusal: () => MutationRefusal;
  demo: boolean;
  run: (ctx: Ctx, form: FormData) => Promise<string | null | MutationRefusal>;
  failed: (err: unknown, form: FormData, ctx: Ctx) => MutationRefusal;
  audited: (ctx: Ctx, detail: string) => void;
  invalid: string;
};

type ActionEvent = { request: Request };

export async function mutateAction<Ctx>(
  event: ActionEvent,
  spec: MutationSpec<Ctx>
): Promise<MutationRefusal | { ok: true }> {
  const ctx = await spec.gate();
  if (ctx === null) return spec.refusal();

  const form = await event.request.formData();
  if (spec.demo) return { ok: true };

  let detail: string | null | MutationRefusal;
  try {
    detail = await spec.run(ctx, form);
  } catch (err) {
    return spec.failed(err, form, ctx);
  }
  if (detail === null) return fail(400, { ok: false, error: spec.invalid });
  if (typeof detail !== 'string') return detail;
  spec.audited(ctx, detail);
  return { ok: true };
}
