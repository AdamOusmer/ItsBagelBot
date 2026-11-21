// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, redirect } from '@sveltejs/kit';
import { translate } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { CLOUD_VOICES } from '@bagel/kit/voice-plan';
import { isOwnerSession } from '#lib/server/owner';
import { voiceGet, voiceSet, type VoiceAllowance } from '#lib/server/services/voice';
import type { Actions, PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ locals: { session } }) => {
  if (!session) throw redirect(302, '/login?next=%2Fvoice');
  if (!isOwnerSession(session)) return { readOnly: true, allowance: null };
  return { readOnly: false, allowance: await bestEffort<VoiceAllowance | null>(voiceGet(session), null) };
};

export const actions = {
  save: async ({ locals: { session, locale }, request }) => {
    if (!session) return fail(401, { error: translate(locale, 'serverErrors.notSignedIn') });
    if (!isOwnerSession(session)) return fail(403, { error: translate(locale, 'serverErrors.ownerOnly') });

    const fields = await request.formData();
    const selectedVoice = fields.get('voice');
    const voice = CLOUD_VOICES.find((name) => name === selectedVoice);
    if (!voice) return fail(400, { error: translate(locale, 'voice.invalidVoice') });

    const enabled = fields.get('enabled');
    if (enabled !== 'true' && enabled !== 'false') {
      return fail(400, { error: translate(locale, 'serverErrors.invalidInput') });
    }
    try {
      await voiceSet(session, { enabled: enabled === 'true', voice });
      return { ok: true };
    } catch {
      return fail(503, { error: translate(locale, 'voice.saveFailed') });
    }
  }
} satisfies Actions;
