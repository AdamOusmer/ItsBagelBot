// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, it } from 'bun:test';
import { classifyVia, isVia } from './via';

it.each([
  ['https://chatgpt.com/', null, 'chatgpt'],
  ['', 'chatgpt.com', 'chatgpt'],
  ['https://gemini.google.com/', null, 'gemini'],
  ['https://www.google.com/', null, 'google'],
  ['https://www.google.co.uk/', null, 'google'],
  ['https://www.bing.com/', null, 'bing'],
  ['https://copilot.microsoft.com/', null, 'copilot'],
  ['https://www.perplexity.ai/', null, 'perplexity'],
  ['https://claude.ai/', null, 'claude'],
  ['https://t.co/abc', null, 'x'],
  ['https://m.youtube.com/', null, 'youtube'],
  ['https://docs.itsbagelbot.com/guide', null, 'docs'],
  ['https://itsbagelbot.com/pricing/', null, 'none'],
  ['https://notgoogle.com/', null, 'other'],
  ['https://example.test/', 'newsletter', 'other'],
  ['', null, 'direct'],
  ['', '', 'direct']
] as const)('classifies referrer %p with utm_source %p as %p', (referrer, utm, want) => {
  expect(classifyVia(referrer, utm)).toBe(want);
});

it.each(['chatgpt', 'none'])('accepts listed source %p', (value) => {
  expect(isVia(value)).toBe(true);
});

it.each(['', 'unknown', 'chatgpt.com', '123456789', null, undefined])('rejects unlisted source %p', (value) => {
  expect(isVia(value)).toBe(false);
});
