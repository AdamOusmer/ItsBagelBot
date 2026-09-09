// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, it } from 'bun:test';
import type { ConnKind } from './connection-state';
import { statusTone, type StatusTone } from './status-tone';

// A table, not a case-by-case assertion: the point of the mapping is that it is
// total over ConnKind, so the compiler flags a new kind here (the Record type
// below is exhaustive) rather than letting it fall through to a warning tone
// nobody chose.
const TONES: Record<ConnKind, StatusTone> = {
  online: 'success',
  degraded: 'error',
  reauth_required: 'error',
  unavailable: 'neutral',
  auth_required: 'warning',
  disabled: 'warning',
  connecting: 'warning',
  sub_unknown: 'warning'
};

describe('statusTone', () => {
  it('maps every connection kind to its tone', () => {
    for (const [kind, tone] of Object.entries(TONES)) {
      expect(statusTone(kind as ConnKind)).toBe(tone);
    }
  });

  it('keeps success for the one healthy state only', () => {
    const successes = Object.entries(TONES).filter(([, tone]) => tone === 'success');
    expect(successes).toEqual([['online', 'success']]);
  });
});
