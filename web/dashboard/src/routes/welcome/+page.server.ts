// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { fail, redirect, type Cookies } from '@sveltejs/kit';
import { setOnboarded } from '#lib/server/services';
import { applyFeaturePreset } from '#lib/server/feature-presets';
import { count } from '@bagel/kit/server/analytics';
import { SERVER_STRATEGIES } from '#lib/server/importer';
import { actionError } from '#lib/server/action-errors';
import { isOwnerSession } from '#lib/server/owner';
import type { Session } from '#lib/server/session';
import { IMPORT_SOURCES, type ImportSource } from '@bagel/kit';
import type { Actions, PageServerLoad } from './$types';

const DEMO = dev && env.DEMO === '1';

const SETUP_PRESETS = new Set(['start-new', 'integrate', 'quiet']);
type SetupPreset = 'start-new' | 'integrate' | 'quiet';
type Counted = SetupPreset | 'import';

const consented = (form: FormData) => form.get('consent') === 'yes';

function termsAccepted(locals: App.Locals): boolean {
  const state = locals.accountState;
  return !!state && 'value' in state && state.value.onboarded;
}

async function tourSession(locals: App.Locals): Promise<Session | null> {
  if (locals.session) return locals.session;
  return DEMO ? (await import('#lib/server/demo-data')).demoSession() : null;
}

function sourceConnected(id: ImportSource, cookies: Cookies): boolean {
  const check = SERVER_STRATEGIES[id].connected;
  if (!check) return false;
  return DEMO || check(cookies);
}

function connectedSources(cookies: Cookies): Record<ImportSource, boolean> {
  return Object.fromEntries(IMPORT_SOURCES.map((id) => [id, sourceConnected(id, cookies)])) as Record<
    ImportSource,
    boolean
  >;
}

function signedIn(locals: App.Locals, permitted: (s: Session) => boolean) {
  const s = locals.session;
  if (!s) return { failure: fail(401, { error: actionError(locals.locale, 'Sign in to finish setup.') }) };
  if (!permitted(s)) return { failure: fail(403, { error: actionError(locals.locale, 'Only the channel owner can finish setup.') }) };
  return { uid: s.user_id };
}

const notImpersonated = (s: Session) => !s.impersonator_id;
const owner = (locals: App.Locals) => signedIn(locals, isOwnerSession);
const signer = (locals: App.Locals) => signedIn(locals, notImpersonated);

function welcomeClosed(s: Session, locals: App.Locals): boolean {
  if (s.impersonator_id) return true;
  return !!s.delegate_of && termsAccepted(locals);
}

async function markOnboarded(save: () => Promise<unknown>) {
  try {
    await save();
    return null;
  } catch {
    count('onboarding.steps', 'save_failed');
    return fail(502, { error: 'Could not save your setup. Try again.' });
  }
}

async function persist(locals: App.Locals, counted: Counted, save: (uid: string) => Promise<unknown>) {
  const who = owner(locals);
  if ('failure' in who) return who.failure;
  const failed = await markOnboarded(() => save(who.uid));
  if (failed) return failed;
  count('onboarding.preset', counted);
  return null;
}

async function finishSetup<R>(
  locals: App.Locals,
  counted: Counted,
  save: (uid: string) => Promise<unknown>,
  finish: () => R
) {
  if (!DEMO) {
    const failed = await persist(locals, counted, save);
    if (failed) return failed;
  }
  return finish();
}

export const load: PageServerLoad = async ({ locals, cookies }) => {
  const s = await tourSession(locals);
  if (!s) throw redirect(302, `/login?next=${encodeURIComponent('/welcome')}`);
  if (welcomeClosed(s, locals)) throw redirect(303, '/');
  count('onboarding.steps', 'viewed');
  return {
    name: s.display_name || s.login,
    connected: connectedSources(cookies),
    accepted: termsAccepted(locals),
    delegate: !!s.delegate_of
  };
};

export const actions: Actions = {
  consent: async ({ locals, request }) => {
    if (!consented(await request.formData())) return consentMissing(locals);
    if (DEMO) return { ok: true };
    const who = signer(locals);
    if ('failure' in who) return who.failure;
    return (await markOnboarded(() => setOnboarded(who.uid, true))) ?? { ok: true };
  },
  done: async ({ locals, request }) => {
    const form = await request.formData();
    if (!consented(form)) return consentMissing(locals);
    const value = form.get('preset');
    if (typeof value !== 'string' || !SETUP_PRESETS.has(value)) {
      return fail(400, { error: actionError(locals.locale, 'Choose a setup.') });
    }
    const preset = value as SetupPreset;
    return finishSetup(
      locals,
      preset,
      async (uid) => {
        await applyFeaturePreset(uid, preset);
        await setOnboarded(uid, true);
      },
      () => {
        throw redirect(303, '/');
      }
    );
  },
  finishImport: async ({ locals, request }) => {
    const form = await request.formData();
    if (!consented(form)) return consentMissing(locals);
    return finishSetup(locals, 'import', (uid) => setOnboarded(uid, true), () => ({ ok: true }));
  }
};

function consentMissing(locals: App.Locals) {
  count('onboarding.steps', 'consent_missing');
  return fail(400, { error: actionError(locals.locale, 'Accept the agreement.') });
}
