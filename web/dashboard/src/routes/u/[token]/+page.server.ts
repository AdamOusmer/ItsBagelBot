// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail } from '@sveltejs/kit';
import { translate, type Locale } from '@bagel/kit/i18n';
import { sameOrigin } from '#lib/server/csrf';
import { setMarketingLink, type LinkChange } from '#lib/server/services';
import { isOneClick, linkFailure, readLink } from '#lib/server/unsubscribe';
import type { Actions, PageServerLoad } from './$types';

type Choice = Pick<LinkChange, 'status' | 'source'>;

export const load: PageServerLoad = async ({ params, setHeaders }) => {
  setHeaders({ 'cache-control': 'no-store', 'referrer-policy': 'no-referrer' });
  return { view: await readLink(params.token) };
};

function choiceFor(form: FormData): Choice | null {
  if (isOneClick(form)) return { status: 'withdrawn', source: 'one_click' };
  const status = form.get('status');
  return status === 'granted' || status === 'withdrawn' ? { status, source: 'preference_page' } : null;
}

function refusal(status: number, locale: Locale, key: string) {
  return fail(status, { error: translate(locale, key) });
}

export const actions: Actions = {
  default: async ({ request, params, url, locals }) => {
    const choice = choiceFor(await request.formData());
    if (!choice) return refusal(400, locals.locale, 'emailPrefs.saveError');
    if (choice.source === 'preference_page' && !sameOrigin({ request, url })) {
      return refusal(403, locals.locale, 'emailPrefs.saveError');
    }
    try {
      await setMarketingLink({ token: params.token, ...choice });
    } catch (err) {
      return linkFailure(err) === 'invalid'
        ? refusal(404, locals.locale, 'emailPrefs.invalid')
        : refusal(502, locals.locale, 'emailPrefs.saveError');
    }
    return { ok: true };
  }
};
