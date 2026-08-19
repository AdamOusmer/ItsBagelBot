// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { redirect } from '@sveltejs/kit';
import type { Session } from './session';

// Gated on the build-time `dev` constant first, so Rollup erases the demo
// fallback below from production builds.
const DEMO = dev && env.DEMO === '1';

// The board a read or write targets: for a delegate it is the owner's board,
// for a normal login the user's own. Every module page and action resolves it
// the same way, which is why this lives here rather than being restated per
// route.
//
// With no session there is no board. In a production build that is a dead end
// — the (app) layout's login redirect is the only legitimate outcome — so it
// redirects rather than falling back to a placeholder id: the old `?? 'demo'`
// tail was not demo-gated, so a sessionless request reaching one of these
// loads issued real RPCs scoped to a board id a real account could one day
// occupy. Only a dev demo build resolves to the fixture board.
export function effectiveId(session: Session | null | undefined): string {
  const id = session?.delegate_of ?? session?.user_id;
  if (id) return id;
  if (DEMO) return 'demo';
  throw redirect(302, '/login');
}
