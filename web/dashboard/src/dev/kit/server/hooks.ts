// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { openSessionCookie as openCookie } from '@bagel/kit/server/hooks';
import { demoSession } from '../../fixtures';

export * from '@bagel/kit/server/hooks';

export function openSessionCookie<S>(...args: Parameters<typeof openCookie<S>>): S | null {
  return openCookie(...args) ?? (demoSession() as S);
}
