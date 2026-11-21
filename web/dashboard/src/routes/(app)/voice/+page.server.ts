// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, redirect } from '@sveltejs/kit';
import { translate } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { RpcError } from '@bagel/kit/server/nats';
import { MAX_CLIP_SECONDS, MAX_SPEECH_RUNES, MIN_CLIP_SECONDS } from '@bagel/kit/voice-plan';
import { listModulesForEditing, patchModule } from '#lib/server/commands-store';
import { moduleEditState } from '#lib/server/module-edit-state';
import { isOwnerSession } from '#lib/server/owner';
import { voiceGet, voiceOverlayRotate, voiceSet } from '#lib/server/services/voice';
import { parseVoiceSave, voiceAlertConfig } from './voice-settings';
import type { Actions, PageServerLoad } from './$types';

async function readAlerts(userId: string) {
  const rows = await listModulesForEditing(userId);
  return rows.find(({ name }) => name === 'alerts');
}

export const load: PageServerLoad = async ({ locals: { session } }) => {
  if (!session) throw redirect(302, '/login?next=%2Fvoice');
  if (!isOwnerSession(session)) return { readOnly: true, locked: false, allowance: null, alerts: null };

  const allowance = await bestEffort(voiceGet(session), null);
  const view = { readOnly: false, locked: allowance?.reason === 'no_plan', allowance };
  if (allowance?.reason !== 'ok') return { ...view, alerts: null };

  const alerts = await bestEffort(readAlerts(session.user_id).then((row) => {
    const { config, revision } = moduleEditState(row);
    return { config: voiceAlertConfig(config), revision };
  }), null);
  return { ...view, alerts };
};

export const actions = {
  save: async ({ locals: { session, locale }, request }) => {
    if (!session) return fail(401, { error: translate(locale, 'serverErrors.notSignedIn') });
    if (!isOwnerSession(session)) return fail(403, { error: translate(locale, 'serverErrors.ownerOnly') });

    const save = parseVoiceSave(await request.formData());
    if (save.error !== null) {
      return fail(400, { error: translate(locale, save.error, {
        min: MIN_CLIP_SECONDS, max: save.error === 'voice.invalidMessage' ? MAX_SPEECH_RUNES : MAX_CLIP_SECONDS
      }) });
    }

    try {
      const allowance = await voiceGet(session);
      if (allowance.reason !== 'ok') return fail(403, { error: translate(locale, 'voice.saveUnavailable') });

      const userId = session.user_id;
      const alerts = await readAlerts(userId);
      await voiceSet(session, save.settings);
      const result = await patchModule({
        userId,
        name: 'alerts',
        isEnabled: alerts?.is_enabled ?? true,
        partial: save.partial,
        expectedRev: save.expectedRev
      });
      if (result.conflict) return fail(409, { conflict: true });
      return { ok: true };
    } catch {
      return fail(503, { error: translate(locale, 'voice.saveFailed') });
    }
  },
  rotateOverlay: async ({ locals: { session, locale }, setHeaders }) => {
    if (!session) return fail(401, { overlayError: translate(locale, 'serverErrors.notSignedIn') });
    if (!isOwnerSession(session)) return fail(403, { overlayError: translate(locale, 'serverErrors.ownerOnly') });

    setHeaders({ 'cache-control': 'no-store' });
    try {
      return { overlayUrl: await voiceOverlayRotate(session) };
    } catch (err) {
      if (err instanceof RpcError && err.code === 'forbidden') {
        return fail(403, { overlayError: translate(locale, 'voice.overlayUnavailable') });
      }
      return fail(503, { overlayError: translate(locale, 'voice.overlayFailed') });
    }
  }
} satisfies Actions;
