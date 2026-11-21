// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineRead, defineWrite } from '@bagel/kit/server/service';
import type { CLOUD_VOICES, MONTHLY_VOICE_MINUTES } from '@bagel/kit/voice-plan';
import { effectiveId } from '#lib/server/board';
import type { Session } from '#lib/server/session';
import { SUB } from './subjects';

type VoiceSettings = {
  enabled: boolean;
  voice: (typeof CLOUD_VOICES)[number];
};

export type VoiceAllowance = VoiceSettings & {
  plan: keyof typeof MONTHLY_VOICE_MINUTES;
  period: string;
  limit_tokens: number;
  used_tokens: number;
  remaining_tokens: number;
  resets_at: string;
  reason?: 'ok' | 'no_plan' | 'inactive' | 'banned';
};

function voiceActor(session: Session) {
  return {
    actor_id: session.impersonator_id ?? session.user_id,
    broadcaster_user_id: effectiveId(session)
  };
}

export const voiceGet = defineRead({
  subject: `${SUB.dashboard}.voice_get`,
  request: voiceActor,
  map: (reply: VoiceAllowance) => reply
});

export const voiceSet = defineWrite({
  subject: `${SUB.dashboard}.voice_set`,
  request: (session: Session, settings: VoiceSettings) => ({ ...voiceActor(session), ...settings }),
  map: (reply: { ok?: boolean }) => {
    if (reply.ok !== true) throw new Error('voice settings write was not acknowledged');
  }
});
