// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { TERMS_VERSION } from '@bagel/kit/consent';
import type { Locale } from '@bagel/kit/i18n';
import { defineRead, defineWrite } from '@bagel/kit/server/service';
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

const owner = (id: string) => ({ broadcaster_user_id: id, actor_id: id });

function refreshState(reply: WriteReply, id: string): void {
  if (!reply.ok) throw new Error('Consent write failed');
  invalidate(`state:${id}`);
}

export const setTermsConsent = defineWrite({
  subject: `${SUB.dashboard}.consent_set`,
  request: (id: string, choice: TermsChoice) => ({
    ...owner(id),
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
  request: (id: string, choice: MarketingChoice) => ({ ...owner(id), ...choice, source: 'settings' }),
  after: refreshState
});
