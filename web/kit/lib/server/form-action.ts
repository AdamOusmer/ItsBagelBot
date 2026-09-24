// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, type ActionFailure } from '@sveltejs/kit';
import type { ActionOk } from '../action-result';

export type MutationRefusal = ActionFailure<ActionOk>;

export type MutationSpec<Ctx> = {
  gate: () => Ctx | null | Promise<Ctx | null>;
  refusal: () => MutationRefusal;
  demo: boolean;
  run: (ctx: Ctx, form: FormData) => Promise<string | null | MutationRefusal>;
  failed: (err: unknown, form: FormData, ctx: Ctx) => MutationRefusal;
  audited: (ctx: Ctx, detail: string) => void;
  invalid: string;
};

export async function mutateAction<Ctx>(
  event: { request: Request },
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
