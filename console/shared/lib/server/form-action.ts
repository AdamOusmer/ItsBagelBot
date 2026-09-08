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

export type MutationSpec<Ctx, E> = {
  /** Authorize and resolve the actor context; null refuses. May throw a redirect. */
  gate: (event: E) => Ctx | null | Promise<Ctx | null>;
  /** The refusal for a null gate (403 for admin, 401 for a signed-out board). */
  refusal: () => MutationRefusal;
  /**
   * True only in a `vite dev` demo build. Passed in rather than read here: the
   * canonical `const DEMO = dev && env.DEMO === '1'` gate has to live in the
   * caller's own file for Rollup to fold it away (see the no-dev-code-in-prod
   * gate scripts), so this module never names the key.
   */
  demo: boolean;
  /** Perform the write. Returns the audit detail, or null for invalid input. */
  run: (ctx: Ctx, form: FormData) => Promise<string | null>;
  /** Map a thrown error to a refusal (and audit it, where the caller audits). */
  failed: (err: unknown, form: FormData, ctx: Ctx) => MutationRefusal;
  /** Record the successful mutation. */
  audited: (ctx: Ctx, detail: string) => void;
  /** Message for input `run` rejected. */
  invalid: string;
};

export function mutateAction<Ctx, E extends { request: Request }>(spec: MutationSpec<Ctx, E>) {
  return async (event: E): Promise<MutationRefusal | { ok: true }> => {
    const ctx = await spec.gate(event);
    if (ctx === null) return spec.refusal();

    const form = await event.request.formData();
    if (spec.demo) return { ok: true };

    let detail: string | null;
    try {
      detail = await spec.run(ctx, form);
    } catch (err) {
      return spec.failed(err, form, ctx);
    }
    if (detail === null) return fail(400, { ok: false, error: spec.invalid });
    spec.audited(ctx, detail);
    return { ok: true };
  };
}
