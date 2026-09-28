// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import { isBotAccount } from './bot-account';

describe('bot grant protection', () => {
  test('recognizes outgress identity without any admin environment', () => {
    expect(isBotAccount('bot', { TWITCH_BOT_USER_ID: ' bot ' })).toBe(true);
    expect(isBotAccount('viewer', { TWITCH_BOT_USER_ID: 'bot' })).toBe(false);
  });
  test('ignores the retired admin-only variable', () => {
    expect(isBotAccount('bot', { ADMIN_BOT_USER_ID: 'bot' })).toBe(false);
    expect(isBotAccount('viewer', {})).toBe(false);
  });
});
