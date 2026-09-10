// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { demoConfigured } from './demo-guard';

describe('demoConfigured', () => {
  test('the enabling value', () => {
    expect(demoConfigured({ DEMO: '1' })).toBe(true);
  });

  // The whole point of the broader question: these never turned demo on (that
  // takes exactly '1'), so on a stripped build they would otherwise boot
  // silently and leave an operator believing the pod is a demo instance.
  test.each(['true', 'yes', 'on', 'demo', '2'])('a value that never enabled demo still refuses: %s', (value) => {
    expect(demoConfigured({ DEMO: value })).toBe(true);
  });

  test.each(['0', 'false', 'off', 'no', 'FALSE', 'Off', ''])('an explicit off passes: %s', (value) => {
    expect(demoConfigured({ DEMO: value })).toBe(false);
  });

  test('absent key passes', () => {
    expect(demoConfigured({ ORIGIN: 'https://dashboard.itsbagelbot.com' })).toBe(false);
  });

  // Exact key only: the lookup must not match a lookalike an operator set for
  // something else.
  test.each(['DEMO_MODE', 'demo', 'IS_DEMO', 'DEMOS'])('a lookalike key passes: %s', (key) => {
    expect(demoConfigured({ [key]: '1' })).toBe(false);
  });
});
