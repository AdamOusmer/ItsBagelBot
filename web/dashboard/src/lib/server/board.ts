// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { redirect } from '@sveltejs/kit';
import type { Session } from './session';

const DEMO = dev && env.DEMO === '1';

export function effectiveId(session: Session | null | undefined): string {
  const id = session?.delegate_of ?? session?.user_id;
  if (id) return id;
  if (DEMO) return 'demo';
  throw redirect(302, '/login');
}
