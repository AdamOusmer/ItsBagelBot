// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { YEAR_SECONDS, cookieOptions } from '#lib/server/cookies';
import { flowDenial, liveCards, liveDelete, liveToggle } from '#lib/server/flow-page';
import { flowShareActions } from '#lib/server/flow-share';
import { broadcasterPremium } from '#lib/server/module-gate';
import { parseFlowRouteId, type FlowRef } from '#lib/flow/ref';
import type { DeleteReply, ToggleReply } from '#lib/flow/replies';
import { flowPreview } from '#lib/flow/thumb';

const DEMO = dev && env.DEMO === '1';

const INTRO_SEEN_COOKIE = 'flows_intro_v2';

const BAD_REQUEST = { ok: false, reason: 'failed' } as const;

export const load: PageServerLoad = async ({ locals, cookies }) => {
  if (!(await broadcasterPremium(locals))) return { locked: true, introSeen: true, flows: [] };
  const flows = DEMO ? (await import('#lib/flow/demo')).demoCards() : await liveCards(locals);
  return { locked: false, introSeen: cookies.get(INTRO_SEEN_COOKIE) === '1', flows };
};

async function readTarget(request: Request): Promise<{ form: FormData; ref: FlowRef } | null> {
  const form = await request.formData();
  const ref = parseFlowRouteId(String(form.get('id') ?? ''));
  return ref ? { form, ref } : null;
}

async function toggle({ request, locals }: { request: Request; locals: App.Locals }): Promise<ToggleReply> {
  const denied = await flowDenial(locals);
  if (denied) return denied;
  const target = await readTarget(request);
  if (!target) return BAD_REQUEST;
  const enabled = target.form.get('enabled') === '1';
  const revision = String(target.form.get('revision') ?? '') || undefined;
  return DEMO ? (await import('#lib/flow/demo')).demoToggle(target.ref, enabled, revision) : liveToggle(locals, target.ref, enabled, revision);
}

async function remove({ request, locals }: { request: Request; locals: App.Locals }): Promise<DeleteReply> {
  const denied = await flowDenial(locals);
  if (denied) return denied;
  const target = await readTarget(request);
  if (!target) return BAD_REQUEST;
  return DEMO ? (await import('#lib/flow/demo')).demoDelete(target.ref) : liveDelete(locals, target.ref);
}

export const actions: Actions = {
  dismissIntro: async ({ cookies, url, locals }) => {
    if (!(await broadcasterPremium(locals))) return { ok: false };
    cookies.set(INTRO_SEEN_COOKIE, '1', cookieOptions(url, YEAR_SECONDS));
    return { ok: true };
  },
  toggle,
  remove,
  ...flowShareActions({
    flowPreview,
    demoFlows: DEMO ? async () => (await import('#lib/flow/demo')).demoExportRows() : undefined
  })
};
