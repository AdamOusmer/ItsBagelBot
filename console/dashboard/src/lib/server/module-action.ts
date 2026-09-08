// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The write half of module-page.ts: what every bespoke module page's POST
// actions do around their one store call.
//
// Six pages (timers, channel points, loyalty, Govee, quotes, song queue) each
// spelled out the same prologue and epilogue per verb -- delegate gate, signed-in
// check, effective board id, drain the form, demo short-circuit, try/catch with a
// logged error, audit line -- so a four-verb page carried it four times and the
// copies had already drifted in what they logged and whether they audited at all.
//
// This binds those to mutateAction (@bagel/shared/server/form-action), which
// pins the ORDER; what is added here is the dashboard's own actor: the board
// being written (the OWNER's id, so a delegate edits the owner's board) plus
// the session that asked (so the audit line names the delegate).
//
// `demo` is a parameter rather than a read here for the same reason moduleLoad
// takes a thunk: the canonical `dev && env.DEMO === '1'` const has to live in
// the caller's own route file for Rollup to fold the branch away (see
// shared/scripts/assert-demo-gated.mjs), so this module never names the key.
import { fail } from '@sveltejs/kit';
import { mutateAction, type MutationRefusal } from '@bagel/shared/server/form-action';
import { logger } from '@bagel/shared/server/logger';
import { auditDashboardImpersonation } from './services';
import { gateModulePage } from './module-gate';
import { effectiveId } from './board';
import type { Session } from './session';

/** The board being written, and who asked for it. */
export type ModuleActor = { uid: string; session: Session | null };

/**
 * One verb's write. Returns the audit detail, null when the submitted form is
 * invalid, or its own refusal when the store answered with a reason the page
 * renders specially (a missing OAuth scope, a parent module still off).
 */
export type ModuleMutation = (uid: string, form: FormData) => Promise<string | null | MutationRefusal>;

export type ModuleActionOpts = {
  /** True only in a `vite dev` demo build; see the header. */
  demo: boolean;
  /** Message for input `run` rejected. */
  invalid?: string;
};

type ActionEvent = { request: Request; locals: App.Locals };

export function moduleAction(
  modId: string,
  op: string,
  run: ModuleMutation,
  opts: ModuleActionOpts
): (event: ActionEvent) => Promise<MutationRefusal | { ok: true }> {
  return (event) =>
    mutateAction<ModuleActor>(event, {
      gate: () => {
        // Defense in depth under the route guard, and the same gate the page's
        // load uses: a delegate without the module's section never reaches a
        // write even if the guard is bypassed.
        gateModulePage(event.locals.session, modId);
        if (!opts.demo && !event.locals.session) return null;
        return { uid: effectiveId(event.locals.session), session: event.locals.session ?? null };
      },
      refusal: () => fail(401, { ok: false, error: 'Not signed in.' }),
      demo: opts.demo,
      run: (actor, form) => run(actor.uid, form),
      // The thrown case is ours (an RPC that timed out, a bug): logged with the
      // verb that failed, answered with a generic line. A reason the broadcaster
      // can act on comes back through `run`'s own refusal instead.
      failed: (err) => {
        logger.error({ err }, `[${modId}] ${op} failed`);
        return fail(400, { ok: false, error: `${op} failed` });
      },
      audited: (actor, detail) => auditDashboardImpersonation(actor.session, `${modId}:${op}`, detail),
      invalid: opts.invalid ?? 'Invalid input.'
    });
}
