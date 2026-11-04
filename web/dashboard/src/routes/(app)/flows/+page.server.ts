// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { error } from '@sveltejs/kit';
import { YEAR_SECONDS, cookieOptions } from '#lib/server/cookies';
import { broadcasterPremium } from '#lib/server/module-gate';

const DEMO = dev && env.DEMO === '1';

const INTRO_SEEN_COOKIE = 'flows_intro_v2';

export const load: PageServerLoad = async ({ locals, cookies }) => {
  if (!DEMO) throw error(404);
  if (!(await broadcasterPremium(locals))) return { locked: true, introSeen: true, flows: [] };
  const [{ DEMO_SYSTEMS }, { specFor }, { flowPreview }] = await Promise.all([
    import('#lib/flow/demo'),
    import('#lib/flow/model'),
    import('#lib/flow/thumb')
  ]);
  return {
    locked: false,
    introSeen: cookies.get(INTRO_SEEN_COOKIE) === '1',
    flows: DEMO_SYSTEMS.map((system) => ({
      id: system.id,
      name: system.name,
      section: system.section,
      description: system.description,
      enabled: system.flow.enabled,
      steps: system.flow.nodes.length,
      when: specFor(system.flow.nodes.find((n) => specFor(n.type)?.kind === 'trigger')?.type ?? '')?.label ?? '',
      preview: flowPreview(system.flow)
    }))
  };
};

export const actions: Actions = {
  dismissIntro: ({ cookies, url }) => {
    if (!DEMO) throw error(404);
    cookies.set(INTRO_SEEN_COOKIE, '1', cookieOptions(url, YEAR_SECONDS));
    return { ok: true };
  }
};
