// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { TERMS_VERSION } from '@bagel/kit/consent';
import type { Locale } from '@bagel/kit/i18n';
import { defineRead, defineWrite } from '@bagel/kit/server/service';
import type { Session } from '../session';
import type { MarketingStatus } from './account-state';
import { invalidate } from './fabric';
import { SUB } from './subjects';

export const maskedContactEmail = defineRead({
  subject: `${SUB.dashboard}.state_get`,
  request: (id: string) => ({ broadcaster_user_id: id, include_masked_email: true }),
  map: (reply: { masked_email?: string }) => reply.masked_email ?? ''
});

type WriteReply = { ok: boolean; marketing_skipped?: 'no_email' | 'unavailable' };
type TermsChoice = { source: 'consent' | 'welcome'; marketing: boolean; locale: Locale; requestId: string };
type MarketingChoice = { status: Exclude<MarketingStatus, 'none'>; locale: Locale };

function ownerContext(session: Session) {
  return {
    broadcaster_user_id: session.delegate_of ?? session.user_id,
    actor_id: session.user_id,
    delegate_of: session.delegate_of,
    impersonator_id: session.impersonator_id
  };
}

function refreshState(reply: WriteReply, session: Session): void {
  if (!reply.ok) throw new Error('Consent write failed');
  invalidate(`state:${session.user_id}`);
}

export const setTermsConsent = defineWrite({
  subject: `${SUB.dashboard}.consent_set`,
  request: (session: Session, choice: TermsChoice) => ({
    ...ownerContext(session),
    terms_version: TERMS_VERSION,
    marketing: choice.marketing,
    source: choice.source,
    locale: choice.locale,
    request_id: choice.requestId
  }),
  after: refreshState
});

export const setMarketing = defineWrite({
  subject: `${SUB.dashboard}.marketing_set`,
  request: (session: Session, choice: MarketingChoice) => ({ ...choice, ...ownerContext(session), source: 'settings' }),
  after: refreshState
});
