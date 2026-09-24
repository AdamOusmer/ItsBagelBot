// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ConnKind } from './connection-state';

export type StatusTone = 'success' | 'warning' | 'error' | 'neutral';

export function statusTone(kind: ConnKind): StatusTone {
  switch (kind) {
    case 'online':
      return 'success';
    case 'degraded':
    case 'reauth_required':
    case 'bot_banned':
      return 'error';
    case 'unavailable':
      return 'neutral';
    default:
      return 'warning';
  }
}
