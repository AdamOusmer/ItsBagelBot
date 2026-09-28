// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';

test('the notification bell widens its hit area on coarse pointers', () => {
  const css = readFileSync(new URL('./notifications.css', import.meta.url), 'utf8');
  expect(css).toMatch(/pointer: coarse[\s\S]*bb-notifications__icon-btn::after/);
});
