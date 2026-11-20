// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { effectiveId } from './board';
import { gateModulePage, gateSection } from './module-gate';
import type { Session } from './session';

const DEMO = dev && env.DEMO === '1';

type ModuleOAuthTarget = { id: string; path: string; section?: string };
type ModuleOAuthActor = { uid: string; session: Session };

export function requireModuleOAuthActor(locals: App.Locals, target: ModuleOAuthTarget): ModuleOAuthActor {
  const { session } = locals;
  if (target.section) gateSection(session, target.section);
  gateModulePage(session, target.id);
  if (DEMO) throw redirect(302, target.path);
  if (!session) throw redirect(302, `/login?next=${target.path}`);
  return { uid: effectiveId(session), session };
}
