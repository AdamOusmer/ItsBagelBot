// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail } from '@sveltejs/kit';
import { actionError } from './action-errors';
import { effectiveId } from './board';

type ActionEvent = { request: Request; locals: App.Locals };

export type ActionContext = {
  uid: string;
  session: App.Locals['session'];
  locale: App.Locals['locale'];
  form: FormData;
};

export const notSignedIn = (locale: App.Locals['locale']) =>
  fail(401, { ok: false, error: actionError(locale, 'Not signed in.') });

export function signedInAction(gate: (session: App.Locals['session']) => void, demo: boolean) {
  return <R>(run: (ctx: ActionContext, event: ActionEvent) => Promise<R>) =>
    async (event: ActionEvent) => {
      const { request, locals } = event;
      gate(locals.session);
      if (!demo && !locals.session) return notSignedIn(locals.locale);
      const ctx: ActionContext = {
        uid: effectiveId(locals.session),
        session: locals.session,
        locale: locals.locale,
        form: await request.formData()
      };
      return run(ctx, event);
    };
}

export function parsePerm<T extends string, F>(raw: unknown, allowed: readonly T[], fallback: F): T | F {
  return (allowed as readonly unknown[]).includes(raw) ? (raw as T) : fallback;
}
