// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { canonicalMinecraftUUID } from './minecraft-id';

const UNDASHED = 'deadbeefdeadbeefdeadbeefdeadbeef';

const cases = [
  { name: 'accepts an undashed spelling', input: UNDASHED, want: UNDASHED },
  { name: 'accepts a dashed uppercase spelling', input: 'DEADBEEF-DEAD-BEEF-DEAD-BEEFDEADBEEF', want: UNDASHED },
  { name: 'rejects a username', input: 'Technoblade', want: null },
  { name: 'rejects short hex', input: 'deadbeef', want: null },
  { name: 'rejects empty input', input: '', want: null }
];

test.each(cases)('canonicalMinecraftUUID ', ({ input, want }) => {
  expect(canonicalMinecraftUUID(input)).toBe(want);
});
