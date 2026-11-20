// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { CLOUD_VOICES, DEFAULT_VOICE, MONTHLY_VOICE_MINUTES } from './voice-plan';

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

test('the cloud voice list and default match the Go policy', () => {
  const list = source.match(/var voices = \[\]string\{([^}]+)\}/)?.[1] ?? '';
  const goVoices = [...list.matchAll(/"([^"]+)"/g)].map((match) => match[1]!);

  expect(new Set(goVoices).size).toBe(30);
  expect(goVoices).toEqual([...CLOUD_VOICES]);
  expect(source.match(/^const DefaultVoice = "([^"]+)"$/m)?.[1]).toBe(DEFAULT_VOICE);
});
