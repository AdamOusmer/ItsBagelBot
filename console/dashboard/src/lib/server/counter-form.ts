// Form-parsing helpers shared by the /counters page actions. Kept out of the
// route module so +page.server.ts stays a thin list of actions: each one reads
// its inputs through these, then calls the loyalty store.
import type { CounterScope } from '@bagel/shared';
import { resolveViewerId, type CounterTarget } from './loyalty-store';

// UserError carries a safe, machine-readable failure code out of an action so
// the route's catch can surface it (anything else stays masked) and the client
// maps it to localized copy.
export class UserError extends Error {}

// normalizeName mirrors the loyalty service: bare key, lower-cased, no "!".
export function normalizeName(raw: unknown): string {
  return String(raw ?? '')
    .trim()
    .replace(/^!/, '')
    .toLowerCase()
    .slice(0, 64);
}

// namedValue reads the (name, integer value) pair the value-writing actions
// share; null when either is missing or non-numeric.
export function namedValue(f: FormData): { name: string; value: number } | null {
  const name = normalizeName(f.get('name'));
  const value = Math.trunc(Number(f.get('value')));
  if (!name || !Number.isFinite(value)) return null;
  return { name, value };
}

// bucketTarget reads the optional (viewer_id, command) that address one stored
// bucket. Returns null only when viewer_id is present but malformed; an empty
// target is valid (an untargeted channel set / reset).
export function bucketTarget(f: FormData): CounterTarget | null {
  const viewerId = String(f.get('viewer_id') ?? '').trim();
  if (viewerId && !/^\d+$/.test(viewerId)) return null;
  return { viewerId, command: normalizeName(f.get('command')) };
}

// bucketLabel renders the audit-line suffix for a targeted write; '' when the
// write is untargeted (the whole counter).
export function bucketLabel(t: CounterTarget): string {
  if (!t.viewerId && !t.command) return '';
  return `[${t.viewerId}${t.command ? ':' + t.command : ''}]`;
}

// resolveAddTarget turns the manual-add form into a bucket target for one
// entry-scoped counter. Command scope keys on the command alone; the viewer
// scopes resolve the typed username to its Twitch id (throwing when no such
// account) and stamp the login. Returns null when the form lacks the key part
// the scope needs — the caller maps that to a validation failure.
export async function resolveAddTarget(
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
