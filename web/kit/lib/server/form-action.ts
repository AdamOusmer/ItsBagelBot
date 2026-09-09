// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The write-action skeleton both consoles' counter pages had rewritten around
// their own gate: authorize, read the form, short-circuit demo, run, map the
// failure, write the audit line, answer `{ ok: true }`.
//
// Template Method, with the varying steps injected (Strategy): the ORDER is the
// part worth having once. Reading the form body before the demo short-circuit
// is not incidental -- a demo POST that never drains `request.formData()` leaves
// the body unconsumed -- and the audit line is written after the run so a
// failure is audited by `failed`, with its error, rather than as a success.
//
// Deliberately NOT a gate on its own: `gate` returns the caller's own actor
// context (an admin identity here, a board id there) because the two consoles
// authorize against different things, and folding them into one predicate is
// what would make this abstraction dishonest.
import { fail, type ActionFailure } from '@sveltejs/kit';
import type { ActionOk } from '../action-result';

/** What a refused or failed mutation answers with. */
export type MutationRefusal = ActionFailure<ActionOk>;

export type MutationSpec<Ctx> = {
  /**
   * Authorize and resolve the actor context; null refuses. May throw a
   * redirect. Takes no argument: the caller closes over its own request event,
   * which is what let the second type parameter go -- it was written as
   * `Parameters<NonNullable<Actions[string]>>[0]` at every call site, i.e. the
   * app's own RequestEvent, and only ever to give `gate` a typed `locals`.
   */
  gate: () => Ctx | null | Promise<Ctx | null>;
  /** The refusal for a null gate (403 for admin, 401 for a signed-out board). */
  refusal: () => MutationRefusal;
  /**
   * True only in a `vite dev` demo build. Passed in rather than read here: the
   * canonical build-time demo gate has to live in the caller's own file for
   * Rollup to fold it away (see shared/scripts/assert-demo-gated.mjs), so this
   * module never names the env key at all.
   */
  demo: boolean;
  /**
   * Perform the write. Returns the audit detail, null for invalid input, or a
   * refusal of its own.
   *
   * The third case is for a store that answers with a REASON the page renders
   * specially rather than throwing -- a channel-points write refused for a
   * missing OAuth scope answers 403 + `missingScope` so the page can show the
   * reconnect CTA. Those pages used to carry the whole skeleton just to keep
   * that one branch; letting `run` hand back its own `fail()` is cheaper than
   * a per-page error class thrown only to be unwrapped in `failed`.
   */
  run: (ctx: Ctx, form: FormData) => Promise<string | null | MutationRefusal>;
  /** Map a thrown error to a refusal (and audit it, where the caller audits). */
  failed: (err: unknown, form: FormData, ctx: Ctx) => MutationRefusal;
  /** Record the successful mutation. */
  audited: (ctx: Ctx, detail: string) => void;
  /** Message for input `run` rejected. */
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
