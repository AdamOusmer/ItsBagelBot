// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineRead, defineWrite } from '@bagel/kit/server/service';
import type { CLOUD_VOICES, MONTHLY_VOICE_MINUTES } from '@bagel/kit/voice-plan';
import { effectiveId } from '#lib/server/board';
import type { Session } from '#lib/server/session';
import { SUB } from './subjects';

export type VoiceSettings = {
  enabled: boolean;
  voice: (typeof CLOUD_VOICES)[number] | '';
  max_seconds: number;
};

export type VoiceAllowance = {
  plan: keyof typeof MONTHLY_VOICE_MINUTES;
  period: string;
  limit_tokens: number;
  used_tokens: number;
  remaining_tokens: number;
  resets_at: string;
  reason: 'ok' | 'no_plan' | 'inactive' | 'banned';
};

function voiceActor(session: Session) {
  return {
    actor_id: session.user_id,
    broadcaster_user_id: effectiveId(session)
  };
}

export const voiceGet = defineRead({
  subject: `${SUB.dashboard}.voice_get`,
  request: voiceActor,
  map: (reply: VoiceAllowance) => reply
});

export type TtsOverlayView = {
  settings: VoiceSettings;
  configured: boolean;
  revision: number;
};

const ownerOf = (session: Session) => ({ user_id: effectiveId(session) });

export const voiceOverlayGet = defineRead({
  subject: `${SUB.overlays}.tts.get`,
  request: ownerOf,
  map: (reply: TtsOverlayView) => reply
});

export const voiceSet = defineWrite({
  subject: `${SUB.overlays}.tts.save`,
  request: (session: Session, save: Pick<TtsOverlayView, 'settings' | 'revision'>) => ({ ...ownerOf(session), ...save }),
  map: (reply: TtsOverlayView) => reply
});

export const voiceOverlayRotate = defineWrite({
  subject: `${SUB.overlays}.tts.rotate`,
  request: ownerOf,
  map: (reply: { url: string }) => reply.url
});

type TestAlertStatus = 'queued' | 'no_link' | 'no_minutes' | 'rate_limited' | 'disabled';

export const voiceTestAlert = defineWrite({
  subject: 'rpc.voice.test',
  request: (session: Session) => ({ ...voiceActor(session), display_name: session.display_name || session.login }),
  map: (reply: { status: TestAlertStatus }) => reply.status
});
