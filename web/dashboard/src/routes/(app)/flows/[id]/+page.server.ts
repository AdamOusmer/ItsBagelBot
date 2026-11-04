// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { error, fail, redirect } from '@sveltejs/kit';
import { newFlow } from '#lib/flow/model';
import { effectiveId } from '#lib/server/board';
import { lookupLogin } from '#lib/server/channel-lookup';
import { goveeStore, type GoveeDevice } from '#lib/server/govee-store';
import { broadcasterPremium } from '#lib/server/module-gate';

const DEMO = dev && env.DEMO === '1';

export type GoveeLights = { keyPresent: boolean; devices: GoveeDevice[]; code?: 'key_rejected' | 'unreachable' };

async function goveeLights(locals: App.Locals, url: URL): Promise<GoveeLights> {
  if (DEMO) {
    const { demoGoveeDevices } = await import('#lib/server/demo-data');
    return { keyPresent: url.searchParams.get('setup') !== '1', devices: demoGoveeDevices() };
  }
  const store = goveeStore(effectiveId(locals.session));
  const { keyPresent } = await store.read();
  if (!keyPresent) return { keyPresent, devices: [] };
  const { devices, code } = await store.listDevices();
  return { keyPresent, devices, code };
}

export const load: PageServerLoad = async ({ params, locals, url, depends }) => {
  if (!DEMO) throw error(404);
  if (!(await broadcasterPremium(locals))) throw redirect(303, '/flows');
  depends('flows:govee');
  const govee = goveeLights(locals, url).catch((): GoveeLights => ({ keyPresent: true, devices: [], code: 'unreachable' }));
  if (params.id === 'new') return { flow: newFlow(), govee };
  const { DEMO_SYSTEMS } = await import('#lib/flow/demo');
  const system = DEMO_SYSTEMS.find((s) => s.id === params.id);
  if (!system) throw error(404);
  return { flow: system.flow, govee };
};

const LOGIN_PATTERN = /^[a-z0-9_]{1,25}$/;
const DEMO_MISSING_LOGIN = 'nosuchuser';

export const actions: Actions = {
  resolveUser: async ({ request, locals }) => {
    if (!DEMO) throw error(404);
    if (!(await broadcasterPremium(locals))) return fail(403, { ok: false, code: 'failed' });
    const login = String((await request.formData()).get('login') ?? '');
    if (!LOGIN_PATTERN.test(login)) return fail(400, { ok: false, code: 'missing' });
    if (DEMO) {
      if (login === DEMO_MISSING_LOGIN) return fail(404, { ok: false, code: 'missing' });
      return { ok: true, userId: '100200300', login };
    }
    const found = await lookupLogin(login);
    if (found === 'missing' || found === 'failed') return fail(found === 'missing' ? 404 : 502, { ok: false, code: found });
    return { ok: true, userId: found.userId, login: found.username };
  }
};
