import type { Actions, PageServerLoad } from './$types';
import type { CounterDef, CounterEntryView, CounterScope } from '@bagel/shared';
import { COUNTER_SCOPES } from '@bagel/shared';
import {
  listCounters,
  createCounter,
  setCounter,
  renameCounter,
  deleteCounter,
  deleteCounterEntry,
  counterEntries,
  getCounter,
  resolveViewerId,
  type CounterTarget
} from '$lib/server/loyalty-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { gateModulePage } from '$lib/server/module-gate';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

// Counters are a catalog-defined Modules tool, so the page and every action
// derive delegate access from the same definition as its tile and route guard.
function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'counters');
}

function demoCounters(): CounterDef[] {
  return [
    { name: 'deaths', scope: 'channel', value: 137 },
    { name: 'hugs', scope: 'viewer', value: 0 },
    { name: 'raids', scope: 'command', value: 0 },
    { name: 'redeems', scope: 'viewer_command', value: 0 }
  ];
}

// demoEntries mirrors what counter.entries returns for each demo counter, so
// the inspector drill-down works offline too.
function demoEntries(name: string): CounterEntryView[] {
  if (name === 'raids') {
    return [
      { viewerId: '0', viewerLogin: '', viewerName: '', command: 'raid', value: 41 },
      { viewerId: '0', viewerLogin: '', viewerName: '', command: 'so', value: 12 }
    ];
  }
  return [
    { viewerId: '101', viewerLogin: 'sesame_sam', viewerName: 'Sesame_Sam', command: name === 'redeems' ? 'hydrate' : '', value: 23 },
    { viewerId: '102', viewerLogin: 'bagel_fan', viewerName: 'Bagel_Fan', command: name === 'redeems' ? 'hydrate' : '', value: 9 }
  ];
}

// normalizeName mirrors the loyalty service: bare key, lower-cased, no "!".
function normalizeName(raw: unknown): string {
  return String(raw ?? '')
    .trim()
    .replace(/^!/, '')
    .toLowerCase()
    .slice(0, 64);
}

// The optional ?c=<name> selects one entry-scoped counter whose stored values
// (the per-viewer buckets) are loaded alongside the list.
export const load: PageServerLoad = async ({ locals, url }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  const selected = normalizeName(url.searchParams.get('c'));
  if (env.DEMO === '1') {
    const demo = demoCounters();
    const sel = demo.some((c) => c.name === selected && c.scope !== 'channel') ? selected : '';
    return { counters: demo, selected: sel, entries: sel ? demoEntries(sel) : [] };
  }

  try {
    const counters = await listCounters(uid);
    let entries: CounterEntryView[] = [];
    if (selected && counters.some((c) => c.name === selected && c.scope !== 'channel')) {
      try {
        entries = await counterEntries(uid, selected, 25);
      } catch {
        /* entries are decorative next to the list */
      }
    }
    return { counters, selected, entries };
  } catch {
    return { counters: [] as CounterDef[], selected: '', entries: [] as CounterEntryView[], degraded: true };
  }
};

// UserError carries a safe, machine-readable failure code through mutate's
// catch (anything unexpected stays masked); the client maps it to localized
// copy.
class UserError extends Error {}

// mutate wraps one POST action with the shared boilerplate: gate, session,
// demo short-circuit, error mapping and the impersonation audit line. run
// returns the audit detail, or null for a validation failure.
type Mutation = (uid: string, f: FormData) => Promise<string | null>;

function mutate(op: string, run: Mutation) {
  return async ({ request, locals }: Parameters<NonNullable<Actions[string]>>[0]) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    if (env.DEMO === '1') return { ok: true };
    let detail: string | null;
    try {
      detail = await run(uid, f);
    } catch (e) {
      if (e instanceof UserError) return fail(400, { ok: false, error: e.message });
      logger.error({ err: e }, `[counters] ${op} failed`);
      return fail(400, { ok: false, error: `${op} failed` });
    }
    if (detail === null) return fail(400, { ok: false, error: 'Invalid counter.' });
    auditDashboardImpersonation(locals.session, `counters:${op}`, detail);
    return { ok: true };
  };
}

// resolveAddTarget turns the manual-add form into a bucket target for one
// entry-scoped counter. Command scope keys on the command alone; the viewer
// scopes resolve the typed username to its Twitch id (throwing when no such
// account) and stamp the login. Returns null when the form lacks the key part
// the scope needs — the caller maps that to a validation failure.
async function resolveAddTarget(
  scope: CounterScope,
  login: string,
  command: string
): Promise<CounterTarget | null> {
  if (scope === 'command') {
    return command ? { viewerId: '', command, viewerLogin: '' } : null;
  }
  if (!login) return null;
  const viewerId = await resolveViewerId(login);
  if (!viewerId) throw new UserError('unknown_user');
  return { viewerId, command, viewerLogin: login };
}

export const actions: Actions = {
  create: mutate('create', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    const scope = String(f.get('scope') ?? 'channel') as CounterScope;
    if (!name || !COUNTER_SCOPES.includes(scope)) return null;
    await createCounter(uid, name, scope);
    return `${name} (${scope})`;
  }),

  // Absolute value for a channel counter; on entry scopes value 0 doubles as
  // the reset (the service deletes every stored bucket). An optional target
  // (viewer_id and/or command) instead writes that one stored bucket.
  set: mutate('set', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    const value = Math.trunc(Number(f.get('value')));
    if (!name || !Number.isFinite(value)) return null;
    const viewerId = String(f.get('viewer_id') ?? '').trim();
    if (viewerId && !/^\d+$/.test(viewerId)) return null;
    const command = normalizeName(f.get('command'));
    const found = await setCounter(uid, name, value, { viewerId, command });
    if (!found) throw new Error('unknown counter');
    const bucket = viewerId || command ? `[${viewerId}${command ? ':' + command : ''}]` : '';
    return `${name}${bucket}=${value}`;
  }),

  // addEntry writes one bucket of an entry-scoped counter from the inspector's
  // manual add. The counter's own scope gates which target parts are required
  // (resolveAddTarget), so a malformed post can never fall through to the
  // untargeted "reset everything" set.
  addEntry: mutate('addEntry', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    const value = Math.trunc(Number(f.get('value')));
    if (!name || !Number.isFinite(value)) return null;
    const counter = await getCounter(uid, name);
    if (!counter || counter.scope === 'channel') return null;
    const login = String(f.get('username') ?? '').trim().replace(/^@/, '').toLowerCase();
    const target = await resolveAddTarget(counter.scope, login, normalizeName(f.get('command')));
    if (!target) return null;
    const found = await setCounter(uid, name, value, target);
    if (!found) throw new Error('unknown counter');
    return `${name}[${target.viewerId || target.command}]=${value}`;
  }),

  rename: mutate('rename', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    const newName = normalizeName(f.get('new_name'));
    if (!name || !newName) return null;
    if (newName === name) return null;
    const found = await renameCounter(uid, name, newName);
    if (!found) throw new Error('unknown counter');
    return `${name}>${newName}`;
  }),

  delete: mutate('delete', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    if (!name) return null;
    await deleteCounter(uid, name);
    return name;
  }),

  // deleteEntry removes one stored bucket of an entry-scoped counter, addressed
  // by viewer_id and/or command. A bucket must be targeted; an address with
  // neither is rejected here (and again by the service) so this can never wipe
  // the whole counter.
  deleteEntry: mutate('deleteEntry', async (uid, f) => {
    const name = normalizeName(f.get('name'));
    if (!name) return null;
    const viewerId = String(f.get('viewer_id') ?? '').trim();
    if (viewerId && !/^\d+$/.test(viewerId)) return null;
    const command = normalizeName(f.get('command'));
    if (!viewerId && !command) return null;
    const found = await deleteCounterEntry(uid, name, { viewerId, command });
    if (!found) throw new Error('unknown counter');
    return `${name}[${viewerId || command}] removed`;
  })
};
