// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { mutateAction } from '@bagel/shared/server/form-action';
import { dev } from '$app/environment';
import { requireAdmin, isManager, type AdminIdentity } from '$lib/server/access';
import {
  botCounterList,
  botCounterCreate,
  botCounterSet,
  botCounterDelete,
  auditAppend,
  type BotCounter
} from '$lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

export type BotCountersBundle = { counters: BotCounter[]; degraded: boolean };

// normalizeName mirrors the loyalty service: bare key, lower-cased, no "!".
// ':' is reserved (the worker's bot-token prefix), so it never enters a name.
function normalizeName(raw: unknown): string {
  return String(raw ?? '')
    .trim()
    .replace(/^!/, '')
    .toLowerCase()
    .slice(0, 64);
}

function validName(name: string): boolean {
  return name.length > 0 && !name.includes(':');
}

export const load: PageServerLoad = async ({ parent }) => {
  const layout = await parent();
  // Bot-global counters are managers-only, like the rest of the Access group.
  if (!isManager(layout.role)) throw redirect(302, '/');

  const bundle: Promise<BotCountersBundle> = DEMO
    ? Promise.resolve({ counters: [{ name: 'feeds', scope: 'bot', value: 12873 }], degraded: false })
    : botCounterList()
        .then((counters) => ({ counters, degraded: false }))
        .catch(() => ({ counters: [], degraded: true }));

  return { bundle };
};

type AuditLine = { action: string; target: string; detail: string; ok: boolean; error?: string };

function audit(admin: AdminIdentity, line: AuditLine): void {
  if (DEMO) return;
  auditAppend({ actor_id: admin.id, actor_login: admin.login, ...line }).catch(() => {});
}

// mutate binds one POST action to the shared write skeleton
// (@bagel/shared/server/form-action): gate, form, demo short-circuit, error
// mapping, audit. The manager identity is this page's actor context; `run`
// returns the audit detail, or null for a validation failure.
type Mutation = (f: FormData) => Promise<string | null>;

function mutate(op: string, run: Mutation) {
  const action = `bot_counter_${op}`;
  return mutateAction<AdminIdentity, Parameters<NonNullable<Actions[string]>>[0]>({
    gate: async ({ locals }) => {
      const admin = await requireAdmin(locals.session);
      return admin && isManager(admin.role) ? admin : null;
    },
    refusal: () => fail(403, { ok: false, error: 'forbidden' }),
    demo: DEMO,
    run: (_admin, f) => run(f),
    // A failed write is audited too, with the name it was attempted on: a
    // rejected bot-counter change is exactly the kind of thing the trail is
    // read for afterwards.
    failed: (e, f, admin) => {
      const error = (e as Error).message;
      audit(admin, { action, target: 'bot', detail: String(f.get('name') ?? ''), ok: false, error });
      return fail(400, { ok: false, error });
    },
    audited: (admin, detail) => audit(admin, { action, target: 'bot', detail, ok: true }),
    invalid: 'Invalid counter.'
  });
}

export const actions: Actions = {
  create: mutate('create', async (f) => {
    const name = normalizeName(f.get('name'));
    if (!validName(name)) return null;
    await botCounterCreate(name);
    return name;
  }),

  set: mutate('set', async (f) => {
    const name = normalizeName(f.get('name'));
    const value = Math.trunc(Number(f.get('value')));
    if (!validName(name) || !Number.isFinite(value)) return null;
    await botCounterSet(name, value);
    return `${name}=${value}`;
  }),

  delete: mutate('delete', async (f) => {
    const name = normalizeName(f.get('name'));
    if (!validName(name)) return null;
    await botCounterDelete(name);
    return name;
  })
};
