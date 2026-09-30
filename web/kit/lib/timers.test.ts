// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { blankTimer, DEFAULT_CHAT_LINES } from './timers';

describe('blankTimer', () => {
  test('new timers default to live only with the chat activity check on', () => {
    const b = blankTimer();
    expect(b.allowOffline).toBe(false);
    expect(b.minChatLines).toBe(DEFAULT_CHAT_LINES);
    expect(b.minChatLines).toBe(15);
    expect(b.chatWindowMinutes).toBe(10);
  });
});
