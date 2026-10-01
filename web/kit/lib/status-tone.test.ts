// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, it } from 'bun:test';
import type { ConnKind } from './connection-state';
import { statusTone, type StatusTone } from './status-tone';

const TONES: Record<ConnKind, StatusTone> = {
  online: 'success',
  degraded: 'danger',
  reauth_required: 'danger',
  bot_banned: 'danger',
  unavailable: 'neutral',
  auth_required: 'warning',
  disabled: 'warning',
  connecting: 'warning',
  sub_unknown: 'warning'
};

describe('statusTone', () => {
  it.each(Object.entries(TONES) as [ConnKind, StatusTone][])('%s reads as %s', (kind, tone) => {
    expect(statusTone(kind)).toBe(tone);
  });
});
