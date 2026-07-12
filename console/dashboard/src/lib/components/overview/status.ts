// Shared status vocabulary for the Overview page. The connection state itself is
// owned by main's honest state machine (`connectionUiState` in @bagel/shared),
// which resolves every backend permutation to exactly one `ConnKind`. This module
// only layers the page's VISUAL tone on top of that kind, so the status panel and
// its dot/colour never disagree with the word they sit beside. Colour is always
// decoration on top of the textual state, never the only signal.
import type { ConnKind } from '@bagel/shared';

export type StatusTone = 'success' | 'warning' | 'error' | 'neutral';

// Map main's ConnKind to a tone. `online` is the only success; `degraded` is the
// only error (connected but not serving chat); a down core read is neutral; every
// mid-flight or not-connected state warns.
export function statusTone(kind: ConnKind): StatusTone {
  switch (kind) {
    case 'online':
      return 'success';
    case 'degraded':
      return 'error';
    case 'unavailable':
      return 'neutral';
    default:
      return 'warning';
  }
}
