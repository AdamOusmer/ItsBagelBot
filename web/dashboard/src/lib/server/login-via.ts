// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { isVia, VIA_PARAM, type Via } from '@bagel/kit/via';

const SEPARATOR = '.';

export function loginVia(url: URL): Via {
  const via = url.searchParams.get(VIA_PARAM);
  return isVia(via) ? via : 'none';
}

// The source rides in the OAuth state round trip so no cookie or device storage carries it.
export function withVia(state: string, via: Via): string {
  return `${state}${SEPARATOR}${via}`;
}

export function splitState(returned: string): { state: string; via: Via } {
  const at = returned.lastIndexOf(SEPARATOR);
  const via = at < 0 ? '' : returned.slice(at + 1);
  return at < 0 || !isVia(via) ? { state: returned, via: 'none' } : { state: returned.slice(0, at), via };
}
