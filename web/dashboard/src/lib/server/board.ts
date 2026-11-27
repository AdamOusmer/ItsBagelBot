// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';
import type { Session } from './session';

export function effectiveId(session: Session | null | undefined): string {
  const id = session?.delegate_of ?? session?.user_id;
  if (id) return id;
  throw redirect(302, '/login');
}
