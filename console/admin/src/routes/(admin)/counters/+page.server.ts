import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
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

function audit(admin: AdminIdentity, action: string, target: string, detail: string, ok: boolean, error?: string): void {
  if (DEMO) return;
  auditAppend({ actor_id: admin.id, actor_login: admin.login, action, target, detail, ok, error }).catch(() => {});
}

// mutate wraps one POST action with the shared boilerplate: manager gate, demo
// short-circuit, error mapping and the audit line. run returns the audit detail,
// or null for a validation failure.
type Mutation = (f: FormData) => Promise<string | null>;

function mutate(op: string, run: Mutation) {
  return async ({ request, locals }: Parameters<NonNullable<Actions[string]>>[0]) => {
    const admin = await requireAdmin(locals.session);
    if (!admin || !isManager(admin.role)) return fail(403, { ok: false, error: 'forbidden' });

    const f = await request.formData();
    if (DEMO) return { ok: true };
    let detail: string | null;
    try {
      detail = await run(f);
    } catch (e) {
      audit(admin, `bot_counter_${op}`, 'bot', String(f.get('name') ?? ''), false, (e as Error).message);
      return fail(400, { ok: false, error: (e as Error).message });
    }
    if (detail === null) return fail(400, { ok: false, error: 'Invalid counter.' });
    audit(admin, `bot_counter_${op}`, 'bot', detail, true);
    return { ok: true };
  };
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
