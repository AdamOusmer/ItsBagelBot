// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { fail, redirect, type Cookies } from '@sveltejs/kit';
import { publishEventSub, setOnboarded } from '#lib/server/services';
import { consentFields, currentAccount, needsTerms, submitTerms } from '#lib/server/consent';
import { applyFeaturePreset } from '#lib/server/feature-presets';
import { count } from '@bagel/kit/server/analytics';
import { SERVER_STRATEGIES } from '#lib/server/importer';
import { actionError } from '#lib/server/action-errors';
import { isOwnerSession } from '#lib/server/owner';
import type { Session } from '#lib/server/session';
import { IMPORT_SOURCES, type ImportSource } from '@bagel/kit';
import { translate, type Locale } from '@bagel/kit/i18n';
import type { Actions, PageServerLoad } from './$types';

const DEMO = dev && env.DEMO === '1';

const SETUP_PRESETS = new Set(['start-new', 'integrate', 'quiet']);
type SetupPreset = 'start-new' | 'integrate' | 'quiet';
type Counted = SetupPreset | 'import';

function termsAccepted(locals: App.Locals): boolean {
  return !needsTerms(currentAccount(locals)?.termsVersion);
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

function owner(locals: App.Locals) {
  const s = locals.session;
  if (!s) return { failure: fail(401, { error: actionError(locals.locale, 'Sign in to finish setup.') }) };
  if (!isOwnerSession(s)) return { failure: fail(403, { error: actionError(locals.locale, 'Only the channel owner can finish setup.') }) };
  return { uid: s.user_id };
}

async function finishOnboarding(locals: App.Locals, uid: Session['user_id']) {
  const account = currentAccount(locals);
  const pendingEnrollment = account?.active && !account.onboarded;
  if (pendingEnrollment) await publishEventSub(uid, true, { flush: true });
  await setOnboarded(uid, true);
}

async function markOnboarded(save: () => Promise<unknown>, locale: Locale) {
  try {
    await save();
    return null;
  } catch {
    count('onboarding.steps', 'save_failed');
    return fail(502, { error: translate(locale, 'onboarding.saveError') });
  }
}

async function persist(locals: App.Locals, counted: Counted, save: (uid: string) => Promise<unknown>) {
  const who = owner(locals);
  if ('failure' in who) return who.failure;
  const failed = await markOnboarded(() => save(who.uid), locals.locale);
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

function recordConsentFailure<T extends { status: number }>(failure: T): T {
  if (failure.status === 400) count('onboarding.steps', 'consent_missing');
  if (failure.status === 502) count('onboarding.steps', 'save_failed');
  return failure;
}

export const load: PageServerLoad = async ({ locals, cookies }) => {
  const s = await tourSession(locals);
  if (!s) throw redirect(302, `/login?next=${encodeURIComponent('/welcome')}`);
  if (!isOwnerSession(s)) throw redirect(303, '/');
  const accepted = termsAccepted(locals);
  count('onboarding.steps', 'viewed');
  return {
    name: s.display_name || s.login,
    connected: connectedSources(cookies),
    accepted,
    consent: accepted ? null : await consentFields(s, locals, DEMO)
  };
};

export const actions: Actions = {
  consent: async ({ locals, request }) => {
    const session = await tourSession(locals);
    const result = await submitTerms({ locals: { ...locals, session }, request }, 'welcome', DEMO);
    if (!('uid' in result)) return recordConsentFailure(result);
    return { ok: true, marketingSkipped: result.marketingSkipped };
  },
  done: async ({ locals, request }) => {
    if (!DEMO && !termsAccepted(locals)) return consentMissing(locals);
    const form = await request.formData();
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
        await finishOnboarding(locals, uid);
      },
      () => {
        throw redirect(303, '/');
      }
    );
  },
  finishImport: async ({ locals }) => {
    if (!DEMO && !termsAccepted(locals)) return consentMissing(locals);
    return finishSetup(locals, 'import', (uid) => finishOnboarding(locals, uid), () => ({ ok: true }));
  }
};

function consentMissing(locals: App.Locals) {
  count('onboarding.steps', 'consent_missing');
  return fail(400, { error: actionError(locals.locale, 'Accept the agreement.') });
}
