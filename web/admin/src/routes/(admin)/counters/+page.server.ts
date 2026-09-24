// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad, RequestEvent } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { mutateAction } from '@bagel/kit/server/form-action';
import { normalizeCounterName } from '@bagel/kit/validation';
import { dev } from '$app/environment';
import { allows, requireRole, type AdminIdentity } from '$lib/server/access';
import { audit } from '$lib/server/audit';
import { actionError, adminText } from '$lib/server/admin-action';
import {
  botCounterList,
  botCounterCreate,
  botCounterSet,
  botCounterDelete,
  type BotCounter
} from '$lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

export type BotCountersBundle = { counters: BotCounter[]; degraded: boolean };

function validName(name: string): boolean {
  return name.length > 0 && !name.includes(':');
}

export const load: PageServerLoad = async ({ parent }) => {
  const layout = await parent();
  // The only gate: the loyalty service checks no role for bot-global counters.
  if (!allows(layout.role, 'counters.manage')) throw redirect(302, '/');

  const bundle: Promise<BotCountersBundle> = DEMO
    ? Promise.resolve({ counters: [{ name: 'feeds', scope: 'bot', value: 12873 }], degraded: false })
    : botCounterList()
        .then((counters) => ({ counters, degraded: false }))
        .catch(() => ({ counters: [], degraded: true }));

  return { bundle };
};

type Mutation = (f: FormData) => Promise<string | null>;

function mutate(op: string, run: Mutation) {
  const action = `bot_counter_${op}`;
  return (event: RequestEvent) =>
    mutateAction<AdminIdentity>(event, {
      gate: () => requireRole(event, 'counters.manage'),
      refusal: () => fail(403, { ok: false, error: actionError(event.locals.locale, 'forbidden') }),
      demo: DEMO,
      run: (_admin, f) => run(f),
      failed: (e, f, admin) => {
        const error = (e as Error).message;
        audit(admin, { action, target: 'bot', detail: String(f.get('name') ?? ''), ok: false, error });
        return fail(400, { ok: false, error });
      },
      audited: (admin, detail) => audit(admin, { action, target: 'bot', detail, ok: true }),
      invalid: adminText(event.locals.locale, 'admin.counters.invalid')
    });
}

export const actions: Actions = {
  create: mutate('create', async (f) => {
    const name = normalizeCounterName(f.get('name'));
    if (!validName(name)) return null;
    await botCounterCreate(name);
    return name;
  }),

  set: mutate('set', async (f) => {
    const name = normalizeCounterName(f.get('name'));
    const value = Math.trunc(Number(f.get('value')));
    if (!validName(name) || !Number.isFinite(value)) return null;
    await botCounterSet(name, value);
    return `${name}=${value}`;
  }),

  delete: mutate('delete', async (f) => {
    const name = normalizeCounterName(f.get('name'));
    if (!validName(name)) return null;
    await botCounterDelete(name);
    return name;
  })
};
