// Action bodies for the /counters page's value-writing verbs. Kept out of the
// route so +page.server.ts stays thin wiring; each returns the audit detail
// (or null for a validation failure) — the shape mutate() expects, and it
// throws on a service-level "unknown counter" so mutate masks it.
import { setCounter, getCounter, deleteCounterEntry } from './loyalty-store';
import { namedValue, bucketTarget, bucketLabel, resolveAddTarget, normalizeName } from './counter-form';

// runSet writes an absolute value: a channel counter's value, or one targeted
// bucket; value 0 on an entry counter with no target is the whole-counter reset.
export async function runSet(uid: string, f: FormData): Promise<string | null> {
  const nv = namedValue(f);
  const target = bucketTarget(f);
  if (!nv || !target) return null;
  const found = await setCounter(uid, nv.name, nv.value, target);
  if (!found) throw new Error('unknown counter');
  return `${nv.name}${bucketLabel(target)}=${nv.value}`;
}

// runAddEntry writes one bucket of an entry-scoped counter from the inspector's
// manual add. The counter's own scope gates which target parts are required
// (resolveAddTarget), so a malformed post can never fall through to the
// untargeted "reset everything" set.
export async function runAddEntry(uid: string, f: FormData): Promise<string | null> {
  const nv = namedValue(f);
  if (!nv) return null;
  const counter = await getCounter(uid, nv.name);
  if (!counter || counter.scope === 'channel') return null;
  const login = String(f.get('username') ?? '').trim().replace(/^@/, '').toLowerCase();
  const target = await resolveAddTarget(counter.scope, login, normalizeName(f.get('command')));
  if (!target) return null;
  const found = await setCounter(uid, nv.name, nv.value, target);
  if (!found) throw new Error('unknown counter');
  return `${nv.name}[${target.viewerId || target.command}]=${nv.value}`;
}

// runDeleteEntry removes one stored bucket, addressed by viewer_id and/or
// command. A bucket must be targeted; an address with neither is rejected here
// (and again by the service) so this can never wipe the whole counter.
export async function runDeleteEntry(uid: string, f: FormData): Promise<string | null> {
  const name = normalizeName(f.get('name'));
  const target = bucketTarget(f);
  if (!name || !target) return null;
  if (!target.viewerId && !target.command) return null;
  const found = await deleteCounterEntry(uid, name, target);
  if (!found) throw new Error('unknown counter');
  return `${name}${bucketLabel(target)} removed`;
}
