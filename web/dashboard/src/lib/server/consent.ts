// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { randomUUID } from 'node:crypto';
import { fail } from '@sveltejs/kit';
import { TERMS_VERSION } from '@bagel/kit/consent';
import { isLocale, translate } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { actionError } from './action-errors';
import { isOwnerSession } from './owner';
import { maskedContactEmail, setTermsConsent } from '#lib/server/services';
import type { Session } from './session';

const REQUEST_ID = /^[\da-f]{8}-(?:[\da-f]{4}-){3}[\da-f]{12}$/i;

export function currentAccount(locals: App.Locals) {
  const state = locals.accountState;
  return state && 'value' in state ? state.value : null;
}

export function needsTerms(version: string | undefined): boolean {
  return (version ?? '') < TERMS_VERSION;
}

export async function consentFields(session: Session, demo = false) {
  return {
    requestId: randomUUID(),
    maskedEmail: demo ? '' : await bestEffort(maskedContactEmail(session.user_id), '')
  };
}

function termsRequestId(form: FormData): string | null {
  const id = form.get('request_id');
  if (form.get('terms_version') !== TERMS_VERSION) return null;
  return typeof id === 'string' && REQUEST_ID.test(id) ? id : null;
}

export async function submitTerms(
  { locals, request }: { locals: App.Locals; request: Request },
  source: 'consent' | 'welcome',
  demo = false
) {
  const { session, locale } = locals;
  if (!session) return fail(401, { error: actionError(locale, 'Not signed in.') });
  if (!isOwnerSession(session)) return fail(403, { error: actionError(locale, 'Not allowed.') });

  const form = await request.formData();
  const requestId = termsRequestId(form);
  if (form.get('consent') !== 'yes') return fail(400, { error: translate(locale, 'onboarding.consentHint') });
  if (!requestId) return fail(400, { error: translate(locale, 'consent.reload') });
  if (demo) return { uid: session.user_id };

  const formLocale = form.get('locale');
  try {
    await setTermsConsent(session.user_id, {
      source,
      marketing: form.get('marketing') === 'yes',
      locale: isLocale(formLocale) ? formLocale : locale,
      requestId
    });
    return { uid: session.user_id };
  } catch {
    return fail(502, { error: translate(locale, 'consent.saveError') });
  }
}
