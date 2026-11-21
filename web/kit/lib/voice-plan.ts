// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const MONTHLY_VOICE_MINUTES = {
  free: 0,
  premium: 60,
  giveaway: 60,
  admin_grant: 30,
  vip: 120
} as const;

export const CLOUD_VOICE_CATALOG = [
  ['Zephyr', 'Bright'],
  ['Puck', 'Upbeat'],
  ['Charon', 'Informative'],
  ['Kore', 'Firm'],
  ['Fenrir', 'Excitable'],
  ['Leda', 'Youthful'],
  ['Orus', 'Firm'],
  ['Aoede', 'Breezy'],
  ['Callirrhoe', 'Easy-going'],
  ['Autonoe', 'Bright'],
  ['Enceladus', 'Breathy'],
  ['Iapetus', 'Clear'],
  ['Umbriel', 'Easy-going'],
  ['Algieba', 'Smooth'],
  ['Despina', 'Smooth'],
  ['Erinome', 'Clear'],
  ['Algenib', 'Gravelly'],
  ['Rasalgethi', 'Informative'],
  ['Laomedeia', 'Upbeat'],
  ['Achernar', 'Soft'],
  ['Alnilam', 'Firm'],
  ['Schedar', 'Even'],
  ['Gacrux', 'Mature'],
  ['Pulcherrima', 'Forward'],
  ['Achird', 'Friendly'],
  ['Zubenelgenubi', 'Casual'],
  ['Vindemiatrix', 'Gentle'],
  ['Sadachbia', 'Lively'],
  ['Sadaltager', 'Knowledgeable'],
  ['Sulafat', 'Warm'],
] as const;

export const CLOUD_VOICES: ReadonlyArray<(typeof CLOUD_VOICE_CATALOG)[number][0]> =
  CLOUD_VOICE_CATALOG.map(([name]) => name);

export const VOICE_GROUPS = {
  Bright: 'energetic',
  Upbeat: 'energetic',
  Excitable: 'energetic',
  Lively: 'energetic',
  Youthful: 'energetic',
  Forward: 'energetic',
  Warm: 'warm',
  Soft: 'warm',
  Gentle: 'warm',
  Breathy: 'warm',
  Breezy: 'warm',
  Friendly: 'warm',
  Smooth: 'calm',
  'Easy-going': 'calm',
  Casual: 'calm',
  Even: 'calm',
  Clear: 'calm',
  Firm: 'authoritative',
  Informative: 'authoritative',
  Knowledgeable: 'authoritative',
  Mature: 'authoritative',
  Gravelly: 'authoritative'
} as const satisfies Record<(typeof CLOUD_VOICE_CATALOG)[number][1], string>;

export const VOICE_STYLES = {
  warm: 'warm, friendly, upbeat',
  hype: 'excited, energetic, celebratory',
  calm: 'calm, gentle, relaxed',
  playful: 'playful, light, amused',
  cozy: 'soft, cozy, intimate'
} as const;

export const ALERT_VOICE_DEFAULTS = {
  follow: { enabled: false, message: '{user}, <short pause> thanks for the follow!', style: 'warm' },
  sub: { enabled: true, message: '{user} just subscribed! <short pause> Welcome to the crew!', style: 'hype' },
  gift: { enabled: true, message: '{user} gifted {count} subs! <short pause> Thank you so much!', style: 'hype' },
  cheer: { enabled: true, message: '{user} cheered {bits} bits!', style: 'playful' },
  raid: { enabled: true, message: '{user} is raiding with {viewers} viewers! <short pause> Welcome in!', style: 'hype' }
} as const;

export const DEFAULT_TTS_MIN_BITS = 100;
export const VOICE_CHARS_PER_SECOND = 14;
export const MAX_SPEECH_RUNES = 500;
export type VoiceAlertKind = keyof typeof ALERT_VOICE_DEFAULTS;
export const VOICE_ALERT_KINDS = Object.keys(ALERT_VOICE_DEFAULTS) as VoiceAlertKind[];

export function alertVoiceKeys(kind: VoiceAlertKind) {
  return {
    enabled: `${kind}Tts`,
    message: `${kind}TtsMessage`,
    style: `${kind}TtsStyle`,
    voice: `${kind}TtsVoice`
  } as const;
}

export const ALERT_VOICE_CONFIG_DEFAULTS = {
  ttsLiveOnly: 'true',
  ttsReadMessages: 'true',
  ttsMinBits: String(DEFAULT_TTS_MIN_BITS),
  ...Object.fromEntries(VOICE_ALERT_KINDS.flatMap((kind) => {
    const keys = alertVoiceKeys(kind);
    const defaults = ALERT_VOICE_DEFAULTS[kind];
    return [
      [keys.enabled, String(defaults.enabled)],
      [keys.message, defaults.message],
      [keys.style, defaults.style],
      [keys.voice, '']
    ];
  }))
} as const;

export const ALERT_VOICE_CONFIG_KEYS = Object.keys(ALERT_VOICE_CONFIG_DEFAULTS) as readonly string[];

export function usageTone(used: number, limit: number) {
  const fraction = limit > 0 ? used / limit : 0;
  if (fraction >= 1) return 'inactive';
  if (fraction >= 0.9) return 'danger';
  if (fraction >= 0.75) return 'warning';
  return 'success';
}

export const DEFAULT_VOICE = 'Kore' satisfies (typeof CLOUD_VOICES)[number];
export const VOICE_TOKENS_PER_SECOND = 25;
export const MIN_CLIP_SECONDS = 5;
export const MAX_CLIP_SECONDS = 120;
