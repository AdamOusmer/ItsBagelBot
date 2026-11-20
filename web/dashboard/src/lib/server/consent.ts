// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { randomUUID } from 'node:crypto';
import { fail } from '@sveltejs/kit';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { isLocale, translate } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { env } from '@bagel/kit/server/env';
import { actionError } from './action-errors';
import { isOwnerSession } from './owner';
import { maskedContactEmail, publishEventSub, setOnboarded, setTermsConsent } from '#lib/server/services';
import type { Session } from './session';

const REQUEST_ID = /^[\da-f]{8}-(?:[\da-f]{4}-){3}[\da-f]{12}$/i;
type TermsSubmission = { source: 'consent' | 'welcome'; requestId: string };

export function currentAccount(locals: App.Locals) {
  const state = locals.accountState;
  return state && 'value' in state ? state.value : null;
}

export function needsTerms(version: string | undefined): boolean {
  return (version ?? '') < TERMS_VERSION;
}

export function businessMailingAddress(): string {
  return env.BUSINESS_MAILING_ADDRESS?.trim() ?? '';
}

export async function consentFields(session: Session, locals: App.Locals, demo = false) {
  const mailingAddress = businessMailingAddress();
  const subscribed = currentAccount(locals)?.marketingStatus === 'granted';
  const maskedEmail = demo || !mailingAddress || subscribed
    ? ''
    : await bestEffort(maskedContactEmail(session.user_id), '');
  return {
    requestId: randomUUID(),
    mailingAddress,
    subscribed,
    maskedEmail
  };
}

function termsRequestId(form: FormData): string | null {
  const id = form.get('request_id');
  if (form.get('terms_version') !== TERMS_VERSION) return null;
  return typeof id === 'string' && REQUEST_ID.test(id) ? id : null;
}

async function startSetup(uid: Session['user_id'], account: ReturnType<typeof currentAccount>) {
  await setOnboarded(uid, true);
  if (account?.active) await publishEventSub(uid, true);
}

async function persistTerms(locals: App.Locals, uid: Session['user_id'], form: FormData, terms: TermsSubmission) {
  const account = currentAccount(locals);
  const formLocale = form.get('locale');
  const locale = isLocale(formLocale) ? formLocale : locals.locale;
  const marketing = !!businessMailingAddress() && account?.marketingStatus !== 'granted' && form.get('marketing') === 'yes';
  try {
    const reply = await setTermsConsent(uid, { ...terms, marketing, locale });
    if (terms.source === 'welcome' || !account?.onboarded) await startSetup(uid, account);
    return { uid, marketingSkipped: reply.marketing_skipped };
  } catch {
    return fail(502, { error: translate(locale, 'consent.saveError') });
  }
}

export async function submitTerms(
  { locals, request }: { locals: App.Locals; request: Request },
  source: TermsSubmission['source'],
  demo = false
) {
  const { session, locale } = locals;
  if (!session) return fail(401, { error: actionError(locale, 'Not signed in.') });
  if (!isOwnerSession(session)) return fail(403, { error: actionError(locale, 'Not allowed.') });

  const form = await request.formData();
  const requestId = termsRequestId(form);
  if (form.get('consent') !== 'yes') return fail(400, { error: translate(locale, 'onboarding.consentHint') });
  if (!requestId) return fail(400, { error: translate(locale, 'consent.reload') });
  if (demo) return { uid: session.user_id, marketingSkipped: undefined };
  return persistTerms(locals, session.user_id, form, { source, requestId });
}
