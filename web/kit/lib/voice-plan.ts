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

export const ALERT_VOICE_CONFIG_DEFAULTS = {
  ttsLiveOnly: 'true',
  ttsReadMessages: 'true',
  ttsMinBits: String(DEFAULT_TTS_MIN_BITS),
  ...Object.fromEntries(Object.entries(ALERT_VOICE_DEFAULTS).flatMap(([kind, defaults]) => [
    [`${kind}Tts`, String(defaults.enabled)],
    [`${kind}TtsMessage`, defaults.message],
    [`${kind}TtsStyle`, defaults.style],
    [`${kind}TtsVoice`, '']
  ]))
} as const;

export const DEFAULT_VOICE = 'Kore' satisfies (typeof CLOUD_VOICES)[number];
export const VOICE_TOKENS_PER_SECOND = 25;
export const MIN_CLIP_SECONDS = 5;
export const MAX_CLIP_SECONDS = 120;
