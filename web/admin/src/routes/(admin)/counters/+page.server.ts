// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad, RequestEvent } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { mutateAction, thrownStatus } from '@bagel/kit/server/form-action';
import { parseCounterValue } from '@bagel/kit/validation';
import { dev } from '$app/env';
import { allows, requireRole, type AdminIdentity } from '#lib/server/access';
import { audit } from '#lib/server/audit';
import { actionError, adminText } from '#lib/server/admin-action';
import { COUNTERS_DATA, counterName } from '#lib/components/counters/counter-draft';
import {
  botCounterList,
  botCounterCreate,
  botCounterSet,
  botCounterDelete,
  type BotCounter
} from '#lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

export type BotCountersBundle = { counters: BotCounter[]; degraded: boolean };

export const load: PageServerLoad = async ({ depends, parent }) => {
  const layout = await parent();
  // The loyalty service trusts this gate for bot-global counters.
  if (!allows(layout.role, 'counters.manage')) throw redirect(302, '/');
  depends(COUNTERS_DATA);

  const bundle: Promise<BotCountersBundle> = DEMO
    ? import('#lib/server/demo-giveaways').then(({ demoBotCounters }) => ({ counters: demoBotCounters, degraded: false }))
    : botCounterList()
        .then((counters) => ({ counters, degraded: false }))
        .catch(() => ({ counters: [], degraded: true }));

  return { bundle };
};

type CounterOperation = 'create' | 'set' | 'delete';
type MutationOutcome = { detail: string; counter?: BotCounter };

export type CounterAction = { ok: boolean; error?: string; counter?: BotCounter };

async function mutateCounter(op: CounterOperation, form: FormData): Promise<MutationOutcome | null> {
  const name = counterName(form.get('name'));
  if (name === null) return null;
  if (op === 'create') return { detail: name, counter: await botCounterCreate(name) };
  if (op === 'delete') {
    await botCounterDelete(name);
    return { detail: name };
  }
  const value = parseCounterValue(form.get('value'));
  if (value === null) return null;
  await botCounterSet(name, value);
  return { detail: `${name}=${value}` };
}

function mutate(op: CounterOperation) {
  const action = `bot_counter_${op}`;
  return async (event: RequestEvent) => {
    let counter: BotCounter | undefined;
    const result = await mutateAction<AdminIdentity>(event, {
      gate: () => requireRole(event, 'counters.manage'),
      refusal: () => fail(403, { ok: false, error: actionError(event.locals.locale, 'forbidden') }),
      demo: DEMO,
      run: async (_admin, f) => {
        const outcome = await mutateCounter(op, f);
        counter = outcome?.counter;
        return outcome?.detail ?? null;
      },
      failed: (e, f, admin) => {
        const error = (e as Error).message;
        audit(admin, { action, target: 'bot', detail: String(f.get('name') ?? ''), ok: false, error });
        return fail(thrownStatus(e), { ok: false, error });
      },
      audited: (admin, detail) => audit(admin, { action, target: 'bot', detail, ok: true }),
      invalid: adminText(event.locals.locale, 'admin.counters.invalid')
    });
    return counter ? { ...result, counter } : result;
  };
}

export const actions: Actions = {
  create: mutate('create'),
  set: mutate('set'),
  delete: mutate('delete')
};
