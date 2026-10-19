// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, type ActionFailure } from '@sveltejs/kit';
import type { ActionOk } from '../action-result';
import { countAction, type ActionOutcome } from './analytics';

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

type ActionEvent = { request: Request; route?: { id: string | null }; url?: URL };

export async function mutateAction<Ctx>(
  event: ActionEvent,
  spec: MutationSpec<Ctx>
): Promise<MutationRefusal | { ok: true }> {
  const ctx = await spec.gate();
  if (ctx === null) return counted(event, 'denied', spec.refusal());

  const form = await event.request.formData();
  if (spec.demo) return { ok: true };

  let detail: string | null | MutationRefusal;
  try {
    detail = await spec.run(ctx, form);
  } catch (err) {
    return counted(event, 'failed', spec.failed(err, form, ctx));
  }
  if (detail === null) return counted(event, 'invalid', fail(400, { ok: false, error: spec.invalid }));
  if (typeof detail !== 'string') return counted(event, 'invalid', detail);
  spec.audited(ctx, detail);
  return counted(event, 'ok', { ok: true as const });
}

function counted<R>(event: ActionEvent, outcome: ActionOutcome, result: R): R {
  countAction(event, outcome);
  return result;
}
