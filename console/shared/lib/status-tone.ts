// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Shared status vocabulary for the connection panels. The connection state
// itself is owned by main's honest state machine (`connectionUiState`), which
// resolves every backend permutation to exactly one `ConnKind`. This module
// only layers the VISUAL tone on top of that kind, so a status panel and its
// dot/colour never disagree with the word they sit beside. Colour is always
// decoration on top of the textual state, never the only signal. It sits in
// shared because both consoles render the same connection verdict and a second
// tone table would let them disagree about what 'degraded' looks like.
import type { ConnKind } from './connection-state';

export type StatusTone = 'success' | 'warning' | 'error' | 'neutral';

// Map main's ConnKind to a tone. `online` is the only success; `degraded` and
// `reauth_required` are errors (the bot is not serving chat and needs help); a
// down core read is neutral; every mid-flight or not-connected state warns.
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
