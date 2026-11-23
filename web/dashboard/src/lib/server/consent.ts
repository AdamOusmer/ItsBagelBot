// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { randomUUID } from 'node:crypto';
import { fail } from '@sveltejs/kit';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { isLocale, translate, type Locale } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { env } from '@bagel/kit/server/env';
import { logger } from '@bagel/kit/server/logger';
import { actionError } from './action-errors';
import { isSelfSignIn } from './owner';
import { freshAccountState, maskedContactEmail, publishEventSub, setOnboarded, setTermsConsent, type AccountState } from '#lib/server/services';
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
  const offersOptIn = !!mailingAddress && !subscribed;
  const maskedEmail = offersOptIn && !demo ? await bestEffort(maskedContactEmail(session.user_id), '') : '';
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

function wantsMarketing(form: FormData, account: AccountState): boolean {
  const canOptIn = !!businessMailingAddress() && account.marketingStatus !== 'granted';
  return canOptIn && form.get('marketing') === 'yes';
}

function upstreamFailed(err: unknown, op: string, locale: Locale) {
  logger.error({ err }, `[consent] ${op} failed`);
  return fail(502, { error: translate(locale, 'consent.saveError') });
}

async function persistTerms(locals: App.Locals, session: Session, form: FormData, terms: TermsSubmission) {
  const uid = session.user_id;
  const formLocale = form.get('locale');
  const locale = isLocale(formLocale) ? formLocale : locals.locale;
  let op = 'state_get';
  try {
    const account = currentAccount(locals) ?? await freshAccountState(uid);
    op = 'consent_set';
    const reply = await setTermsConsent(session, { ...terms, marketing: wantsMarketing(form, account), locale });
    const setup = terms.source === 'welcome' || !account.onboarded;
    if (setup && account.active) {
      op = 'eventsub publish';
      await publishEventSub(uid, true, { flush: true });
    }
    if (setup) {
      op = 'onboarded_set';
      await setOnboarded(uid, true);
    }
    return { uid, marketingSkipped: reply.marketing_skipped };
  } catch (err) {
    return upstreamFailed(err, op, locale);
  }
}

export async function submitTerms(
  { locals, request }: { locals: App.Locals; request: Request },
  source: TermsSubmission['source'],
  demo = false
) {
  const { session, locale } = locals;
  if (!session) return fail(401, { error: actionError(locale, 'Not signed in.') });
  if (!isSelfSignIn(session)) return fail(403, { error: actionError(locale, 'Not allowed.') });

  const form = await request.formData();
  const requestId = termsRequestId(form);
  if (form.get('consent') !== 'yes') return fail(400, { error: translate(locale, 'onboarding.consentHint') });
  if (!requestId) return fail(400, { error: translate(locale, 'consent.reload') });
  if (demo) return { uid: session.user_id, marketingSkipped: undefined };
  return persistTerms(locals, session, form, { source, requestId });
}
