// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const { payloadOf, refusalTextOf, succeeded } = await import('./action-result');

const t = ((key: string, params?: Record<string, string>) =>
  params ? `${key}:${Object.values(params).join('|')}` : key) as Parameters<typeof refusalTextOf>[0];

test('invalid refusal lists the fields, unknown ones by name', () => {
  const text = refusalTextOf(t, { code: 'invalid', fields: ['voiceHubId', 'nope'] }, 'x', 'voice');
  expect(text).toBe('discord.errors.invalidFields:Hub channel, nope');
});

test('known code maps to its message, unknown falls back', () => {
  expect(refusalTextOf(t, { code: 'conflict' }, 'x')).toBe('discord.errors.conflict');
  expect(refusalTextOf(t, { code: 'weird' }, 'fallback')).toBe('fallback');
  expect(refusalTextOf(t, { error: 'raw' }, '')).toBe('raw');
});

test('success needs a success result without ok=false', () => {
  expect(succeeded({ type: 'success' }, { ok: true, version: 3 })).toBe(true);
  expect(succeeded({ type: 'success' }, { ok: false })).toBe(false);
  expect(succeeded({ type: 'failure' }, undefined)).toBe(false);
  expect(payloadOf({ type: 'success', data: { ok: true, version: 2 } })?.version).toBe(2);
});
