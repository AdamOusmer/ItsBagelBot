// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail } from '@sveltejs/kit';
import { mutateAction, type MutationRefusal } from '@bagel/kit/server/form-action';
import { logger } from '@bagel/kit/server/logger';
import { auditDashboardImpersonation } from './services';
import { gateModulePage } from './module-gate';
import { effectiveId } from './board';
import type { Session } from './session';
import { actionError } from './action-errors';

export type ModuleActor = { uid: string; session: Session | null };

export type ModuleMutation = (
  uid: string,
  form: FormData,
  locals: App.Locals
) => Promise<string | null | MutationRefusal>;

export type ModuleActionOpts = {
  demo: boolean;
  invalid?: string;
  auditAs?: string;
};

type ActionEvent = { request: Request; locals: App.Locals };

export function moduleAction(
  modId: string,
  op: string,
  run: ModuleMutation,
  opts: ModuleActionOpts
): (event: ActionEvent) => Promise<MutationRefusal | { ok: true }> {
  return async (event) => {
    const result = await mutateAction<ModuleActor>(event, {
      gate: () => {
        gateModulePage(event.locals.session, modId);
        if (!opts.demo && !event.locals.session) return null;
        return { uid: effectiveId(event.locals.session), session: event.locals.session ?? null };
      },
      refusal: () => fail(401, { ok: false, error: 'Not signed in.' }),
      demo: opts.demo,
      run: (actor, form) => run(actor.uid, form, event.locals),
      failed: (err) => {
        logger.error({ err }, `[${modId}] ${op} failed`);
        return fail(400, { ok: false, error: 'Could not update. Try again in a moment.' });
      },
      audited: (actor, detail) =>
        auditDashboardImpersonation(actor.session, `${opts.auditAs ?? modId}:${op}`, detail),
      invalid: opts.invalid ?? 'Invalid input.'
    });
    if ('data' in result && result.data.error) {
      return fail(result.status, { ...result.data, error: actionError(event.locals.locale, result.data.error) });
    }
    return result;
  };
}
