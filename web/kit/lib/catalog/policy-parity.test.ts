// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { FREEZE_MAX_MINUTES } from './loyalty';

const goPolicy = process.env.LOYALTY_POLICY_GO
  ?? join(import.meta.dir, '../../../../internal/domain/rpc/loyalty/policy.go');

const pins = [
  { name: 'FreezeMaximum', web: FREEZE_MAX_MINUTES, go: /^\s*FreezeMaximum\s*=\s*(\d+)\s*\*\s*time\.Minute\s*$/m }
];

test.each(pins)('$name matches the Go policy', ({ web, go }) => {
  const minutes = go.exec(readFileSync(goPolicy, 'utf8'))?.[1];
  expect(Number(minutes)).toBe(web);
});
