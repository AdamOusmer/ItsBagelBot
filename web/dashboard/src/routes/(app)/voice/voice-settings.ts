// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { PERMS } from '@bagel/kit';
import {
  ALERT_VOICE_CONFIG_DEFAULTS, ALERT_VOICE_CONFIG_KEYS, alertVoiceKeys, CLOUD_VOICES, MAX_CLIP_SECONDS, MAX_SPEECH_RUNES,
  MIN_CLIP_SECONDS, TTS_COMMAND_CONFIG_DEFAULTS, TTS_COMMAND_CONFIG_KEYS, VOICE_ALERT_KINDS, VOICE_STYLES
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
  style: (value) => Object.hasOwn(VOICE_STYLES, value) ? null : 'voice.invalidStyle',
  voice: (value) => value === '' || isVoice(value) ? null : 'voice.invalidVoice',
  bits: (value) => Number.isNaN(unsignedInteger(value)) ? 'voice.invalidMinBits' : null
} satisfies Record<string, FieldValidator>;

const alertValidators = new Map<string, FieldValidator>([
  ['ttsLiveOnly', validators.enabled],
  ['ttsMinBits', validators.bits],
  ...VOICE_ALERT_KINDS.flatMap((kind) => Object.entries(alertVoiceKeys(kind)).map(([field, key]) =>
    [key, validators[field as keyof ReturnType<typeof alertVoiceKeys>]] as const))
]);

function maxSecondsError(value: string) {
  const seconds = unsignedInteger(value);
  return seconds >= MIN_CLIP_SECONDS && seconds <= MAX_CLIP_SECONDS ? null : 'voice.invalidMaxLength';
}

const accountValidators = {
  voice: (value) => isVoice(value) ? null : 'voice.invalidVoice',
  enabled: validators.enabled,
  max_seconds: maxSecondsError
} satisfies Record<string, FieldValidator>;

type AccountKey = keyof typeof accountValidators;
const ACCOUNT_KEYS = Object.keys(accountValidators) as AccountKey[];

function presentFields(fields: FormData, keys: readonly string[]): Record<string, string> {
  const present: Record<string, string> = {};
  for (const key of keys) {
    const value = fields.get(key);
    if (typeof value === 'string') present[key] = value;
  }
  return present;
}

function firstError(values: Record<string, string>, validatorFor: (key: string) => FieldValidator | undefined) {
  for (const [key, value] of Object.entries(values)) {
    const error = validatorFor(key)?.(value);
    if (error) return error;
  }
  return null;
}

function accountSettings(values: Record<string, string>) {
  const settings: { voice?: (typeof CLOUD_VOICES)[number]; enabled?: boolean; max_seconds?: number } = {};
  if (isVoice(values.voice)) settings.voice = values.voice;
  if ('enabled' in values) settings.enabled = values.enabled === 'true';
  if ('max_seconds' in values) settings.max_seconds = Number(values.max_seconds);
  return settings;
}

function trimmed(values: Record<string, string>) {
  return Object.fromEntries(Object.entries(values).map(([key, value]) => [key, value.trim()]));
}

function validRevision(expectedRev: number | null, hasAlerts: boolean) {
  if (expectedRev === null) return !hasAlerts;
  return !Number.isNaN(expectedRev);
}

export function ttsCommandConfig(stored: Record<string, string>): Record<string, string> {
  const defaults: Record<string, string> = TTS_COMMAND_CONFIG_DEFAULTS;
  return Object.fromEntries(TTS_COMMAND_CONFIG_KEYS.map((key) => [key, stored[key] ?? defaults[key]]));
}

const MAX_COOLDOWN_SECONDS = 86_400;

const inRange = (min: number, max: number, error: string): FieldValidator => (value) => {
  const integer = unsignedInteger(value);
  return integer >= min && integer <= max ? null : error;
};

const commandValidators = new Map<string, FieldValidator>([
  ['enabled', validators.enabled],
  ['permission', (value) => PERMS.some((perm) => perm === value) ? null : 'serverErrors.invalidInput'],
  ['cooldown', inRange(0, MAX_COOLDOWN_SECONDS, 'voice.invalidCooldown')],
  ['userCooldown', inRange(0, MAX_COOLDOWN_SECONDS, 'voice.invalidCooldown')],
  ['maxChars', inRange(1, MAX_SPEECH_RUNES, 'voice.invalidMaxChars')],
  ['prefixName', validators.enabled],
  ['replyDenied', validators.enabled],
  ['voice', validators.voice],
  ['style', validators.style]
]);

const COMMAND_KEYS = [...commandValidators.keys()];

export function parseCommandSave(fields: FormData) {
  const values = trimmed(presentFields(fields, COMMAND_KEYS));
  const revision = fields.get('expected_rev');
  const expectedRev = revision === null ? null : unsignedInteger(revision);
  const badRevision = expectedRev === null || Number.isNaN(expectedRev);
  if (Object.keys(values).length === 0 || badRevision) {
    return { error: 'serverErrors.invalidInput' } as const;
  }
  const error = firstError(values, (key) => commandValidators.get(key));
  if (error) return { error } as const;
  const { enabled, ...partial } = values;
  return { error: null, enabled: enabled === undefined ? null : enabled === 'true', expectedRev, partial } as const;
}

export function parseVoiceSave(fields: FormData) {
  const account = presentFields(fields, ACCOUNT_KEYS);
  const alerts = presentFields(fields, ALERT_VOICE_CONFIG_KEYS);
  const hasAlerts = Object.keys(alerts).length > 0;
  const revision = fields.get('expected_rev');
  const expectedRev = revision === null ? null : unsignedInteger(revision);
  const hasFields = hasAlerts || Object.keys(account).length > 0;
  if (!hasFields || !validRevision(expectedRev, hasAlerts)) return { error: 'serverErrors.invalidInput' } as const;
  const error = firstError(account, (key) => accountValidators[key as AccountKey])
    ?? firstError(alerts, (key) => alertValidators.get(key));
  if (error) return { error } as const;
  return { error: null, settings: accountSettings(account), expectedRev, partial: trimmed(alerts) } as const;
}

export const utcDateTime = (locale: string) => new Intl.DateTimeFormat(locale, {
  year: 'numeric', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit', timeZone: 'UTC', timeZoneName: 'short'
});
