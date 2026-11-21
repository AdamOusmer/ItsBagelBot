// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import {
  CLOUD_VOICES, DEFAULT_VOICE, MAX_CLIP_SECONDS, MIN_CLIP_SECONDS, MONTHLY_VOICE_MINUTES, VOICE_TOKENS_PER_SECOND
} from './voice-plan';

const source = await Bun.file(new URL('../../../internal/domain/voiceplan/voiceplan.go', import.meta.url)).text();

test('monthly voice minutes match the Go policy', () => {
  const planNames = new Map([...source.matchAll(/^\s*(\w+)\s+Plan\s*=\s*"([a-z_]+)"$/gm)].map((match) => [match[1]!, match[2]!]));
  const table = source.match(/var monthlyMinutes = map\[Plan\]int64\{([^}]+)\}/)?.[1] ?? '';
  const entries = [...table.matchAll(/(\w+):\s*(\d+),/g)];
  const goMinutes = Object.fromEntries(entries.map((match) => [planNames.get(match[1]!), Number(match[2])]));

  expect(planNames.size).toBe(Object.keys(MONTHLY_VOICE_MINUTES).length);
  expect(entries).toHaveLength(planNames.size);
  expect(goMinutes).toEqual(MONTHLY_VOICE_MINUTES);
});

test('the cloud voice list matches the Go policy', () => {
  const list = source.match(/var voices = \[\]string\{([^}]+)\}/)?.[1] ?? '';
  const goVoices = [...list.matchAll(/"([^"]+)"/g)].map((match) => match[1]!);

  expect(new Set(goVoices).size).toBe(30);
  expect(goVoices).toEqual([...CLOUD_VOICES]);
});

test.each([
  { name: 'DefaultVoice', value: `"${DEFAULT_VOICE}"` },
  { name: 'TokensPerSecond', value: String(VOICE_TOKENS_PER_SECOND) },
  { name: 'MinClipSeconds', value: String(MIN_CLIP_SECONDS) },
  { name: 'MaxClipSeconds', value: String(MAX_CLIP_SECONDS) }
])('$name matches the Go policy', ({ name, value }) => {
  const literal = source.match(new RegExp(`^\\s*(?:const\\s+)?${name}\\s*=\\s*(\\S+)$`, 'm'))?.[1];
  expect(literal).toBe(value);
});
