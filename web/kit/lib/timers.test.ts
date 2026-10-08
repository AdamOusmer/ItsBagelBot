// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { blankTimer } from './timers';

test('new timers default to live only with the chat activity check on', () => {
  expect(blankTimer()).toMatchObject({ allowOffline: false, minChatLines: 15, chatWindowMinutes: 10 });
});
