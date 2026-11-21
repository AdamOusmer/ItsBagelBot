// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { ALERTS_MODULE, ALERT_VOICE_CONFIG_DEFAULTS } from './catalog/alerts';
import {
  ALERT_VOICE_DEFAULTS, CLOUD_VOICE_CATALOG, CLOUD_VOICES, DEFAULT_TTS_MIN_BITS, DEFAULT_VOICE,
  MAX_CLIP_SECONDS, MAX_SPEECH_RUNES, MIN_CLIP_SECONDS, MONTHLY_VOICE_MINUTES,
  usageTone, VOICE_CHARS_PER_SECOND, VOICE_STYLES, VOICE_TOKENS_PER_SECOND
} from './voice-plan';

const source = await Bun.file(new URL('../../../internal/domain/voiceplan/voiceplan.go', import.meta.url)).text();
const alerts = await Bun.file(new URL('../../../internal/domain/voiceplan/alerts.go', import.meta.url)).text();

test.each([
  { used: 0, limit: 0, tone: 'success' },
  { used: 74, limit: 100, tone: 'success' },
  { used: 75, limit: 100, tone: 'warning' },
  { used: 89, limit: 100, tone: 'warning' },
  { used: 90, limit: 100, tone: 'danger' },
  { used: 99, limit: 100, tone: 'danger' },
  { used: 100, limit: 100, tone: 'inactive' },
  { used: 101, limit: 100, tone: 'inactive' }
])('maps $used of $limit tokens to $tone', ({ used, limit, tone }) => {
  expect(usageTone(used, limit)).toBe(tone);
});

test('monthly voice minutes match the Go policy', () => {
  const planNames = new Map([...source.matchAll(/^\s*(\w+)\s+Plan\s*=\s*"([a-z_]+)"$/gm)].map((match) => [match[1]!, match[2]!]));
  const table = source.match(/var monthlyMinutes = map\[Plan\]int64\{([^}]+)\}/)?.[1] ?? '';
  const entries = [...table.matchAll(/(\w+):\s*(\d+),/g)];
  const goMinutes = Object.fromEntries(entries.map((match) => [planNames.get(match[1]!), Number(match[2])]));

  expect(planNames.size).toBe(Object.keys(MONTHLY_VOICE_MINUTES).length);
  expect(entries).toHaveLength(planNames.size);
  expect(goMinutes).toEqual(MONTHLY_VOICE_MINUTES);
});

test('cloud voices and character names match the Go catalog', () => {
  const list = source.match(/var voices = \[\]Voice\{\n([\s\S]*?)\n\}/)?.[1] ?? '';
  const entries = [...list.matchAll(/\{"([^"]+)", "([^"]+)"\}/g)].map(([, name, character]) => [name, character]);

  expect(new Set(CLOUD_VOICES).size).toBe(30);
  expect(entries).toEqual([...CLOUD_VOICE_CATALOG]);
});

test('style presets and spoken alert defaults match the Go policy', () => {
  const styles = alerts.match(/var styles = map\[string\]string\{([^}]+)\}/)?.[1] ?? '';
  const goStyles = Object.fromEntries([...styles.matchAll(/"([^"]+)":\s*"([^"]+)"/g)].map(([, id, style]) => [id, style]));
  const rows = [...alerts.matchAll(/^\s*"([a-z]+)":\s*\{(true|false), "([^"]+)", "([^"]+)"\},$/gm)];
  const goDefaults = Object.fromEntries(rows.map(([, id, enabled, message, style]) =>
    [id, { enabled: enabled === 'true', message, style }]));

  expect(goStyles).toEqual(VOICE_STYLES);
  expect(goDefaults).toEqual(ALERT_VOICE_DEFAULTS);
});

test('voice config is separate from the Chat Alerts form', () => {
  const genericKeys = [
    ...(ALERTS_MODULE.settings ?? []).map(({ key }) => key),
    ...ALERTS_MODULE.replies.flatMap(({ enableKey, messageKey }) => [enableKey, messageKey])
  ];
  const voiceKeys = Object.keys(ALERT_VOICE_CONFIG_DEFAULTS);
  expect(voiceKeys).toHaveLength(23);
  expect(genericKeys.filter((key) => key && voiceKeys.includes(key))).toEqual([]);
});

test.each([
  { name: 'DefaultVoice', value: `"${DEFAULT_VOICE}"` },
  { name: 'TokensPerSecond', value: String(VOICE_TOKENS_PER_SECOND) },
  { name: 'MinClipSeconds', value: String(MIN_CLIP_SECONDS) },
  { name: 'MaxClipSeconds', value: String(MAX_CLIP_SECONDS) },
  { name: 'CharsPerSecond', value: String(VOICE_CHARS_PER_SECOND) },
  { name: 'MaxSpeechRunes', value: String(MAX_SPEECH_RUNES) },
  { name: 'DefaultMinBits', value: String(DEFAULT_TTS_MIN_BITS) },
  { name: 'DefaultLiveOnly', value: ALERT_VOICE_CONFIG_DEFAULTS.ttsLiveOnly },
  { name: 'DefaultReadMessages', value: ALERT_VOICE_CONFIG_DEFAULTS.ttsReadMessages }
])('$name matches the Go policy', ({ name, value }) => {
  const literal = (source + alerts).match(new RegExp(`^\\s*(?:const\\s+)?${name}\\s*=\\s*(\\S+)$`, 'm'))?.[1];
  expect(literal).toBe(value);
});
