// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { demoConfigured } from './demo-guard';

describe('demoConfigured', () => {
  test('the enabling value', () => {
    expect(demoConfigured({ DEMO: '1' })).toBe(true);
  });

  test.each(['true', 'yes', 'on', 'demo', '2'])('a value that never enabled demo still refuses: %s', (value) => {
    expect(demoConfigured({ DEMO: value })).toBe(true);
  });

  test.each(['0', 'false', 'off', 'no', 'FALSE', 'Off', ''])('an explicit off passes: %s', (value) => {
    expect(demoConfigured({ DEMO: value })).toBe(false);
  });

  test('absent key passes', () => {
    expect(demoConfigured({ ORIGIN: 'https://dashboard.itsbagelbot.com' })).toBe(false);
  });

  test.each(['DEMO_MODE', 'demo', 'IS_DEMO', 'DEMOS'])('a lookalike key passes: %s', (key) => {
    expect(demoConfigured({ [key]: '1' })).toBe(false);
  });
});
