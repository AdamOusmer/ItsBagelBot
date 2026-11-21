// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  ALERT_VOICE_CONFIG_DEFAULTS, ALERT_VOICE_CONFIG_KEYS, alertVoiceKeys, CLOUD_VOICES,
  MAX_CLIP_SECONDS, MAX_SPEECH_RUNES, MIN_CLIP_SECONDS, VOICE_ALERT_KINDS, VOICE_STYLES
} from '@bagel/kit/voice-plan';

export function voiceAlertConfig(stored: Record<string, string>): Record<string, string> {
  const defaults: Record<string, string> = ALERT_VOICE_CONFIG_DEFAULTS;
  return Object.fromEntries(ALERT_VOICE_CONFIG_KEYS.map((key) => [key, stored[key] ?? defaults[key]]));
}

function unsignedInteger(value: FormDataEntryValue | null): number {
  if (typeof value !== 'string' || !/^\d+$/.test(value)) return NaN;
  const integer = Number(value);
  return Number.isSafeInteger(integer) ? integer : NaN;
}

const isSwitch = (value: FormDataEntryValue | null) => value === 'true' || value === 'false';
const isVoice = (value: unknown): value is (typeof CLOUD_VOICES)[number] => CLOUD_VOICES.some((name) => name === value);
type FieldValidator = (value: string) => string | null;

const validators = {
  enabled: (value) => isSwitch(value) ? null : 'serverErrors.invalidInput',
  message: (value) => value.length <= MAX_SPEECH_RUNES ? null : 'voice.invalidMessage',
  style: (value) => Object.hasOwn(VOICE_STYLES, value) ? null : 'voice.invalidStyle',
  voice: (value) => value === '' || isVoice(value) ? null : 'voice.invalidVoice',
  bits: (value) => Number.isNaN(unsignedInteger(value)) ? 'voice.invalidMinBits' : null
} satisfies Record<string, FieldValidator>;

const alertValidators = new Map<string, FieldValidator>([
  ['ttsLiveOnly', validators.enabled],
  ['ttsReadMessages', validators.enabled],
  ['ttsMinBits', validators.bits],
  ...VOICE_ALERT_KINDS.flatMap((kind) => Object.entries(alertVoiceKeys(kind)).map(([field, key]) =>
    [key, validators[field as keyof ReturnType<typeof alertVoiceKeys>]] as const))
]);

function parseAlerts(fields: FormData): { error: string } | { partial: Record<string, string> } {
  const partial: Record<string, string> = {};
  for (const key of ALERT_VOICE_CONFIG_KEYS) {
    const value = fields.get(key);
    if (typeof value !== 'string') return { error: 'serverErrors.invalidInput' };
    const error = alertValidators.get(key)?.(value);
    if (error) return { error };
    partial[key] = value.trim();
  }
  return { partial };
}

export function parseVoiceSave(fields: FormData) {
  const voice = fields.get('voice');
  if (!isVoice(voice)) return { error: 'voice.invalidVoice' } as const;
  const enabled = fields.get('enabled');
  const expectedRev = unsignedInteger(fields.get('expected_rev'));
  if (!isSwitch(enabled) || Number.isNaN(expectedRev)) return { error: 'serverErrors.invalidInput' } as const;
  const seconds = unsignedInteger(fields.get('max_seconds'));
  if (!(seconds >= MIN_CLIP_SECONDS && seconds <= MAX_CLIP_SECONDS)) return { error: 'voice.invalidMaxLength' } as const;
  const alerts = parseAlerts(fields);
  if ('error' in alerts) return alerts;
  return {
    error: null,
    settings: { voice, enabled: enabled === 'true', max_seconds: seconds },
    expectedRev,
    partial: alerts.partial
  } as const;
}
