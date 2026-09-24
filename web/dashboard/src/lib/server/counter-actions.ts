// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { setCounter, getCounter, deleteCounterEntry } from './loyalty-store';
import { namedValue, bucketTarget, bucketLabel, resolveAddTarget, normalizeCounterName } from './counter-form';

export async function runSet(uid: string, f: FormData): Promise<string | null> {
  const nv = namedValue(f);
  const target = bucketTarget(f);
  if (!nv || !target) return null;
  const found = await setCounter(uid, nv.name, nv.value, target);
  if (!found) throw new Error('unknown counter');
  return `${nv.name}${bucketLabel(target)}=${nv.value}`;
}

export async function runAddEntry(uid: string, f: FormData): Promise<string | null> {
  const nv = namedValue(f);
  if (!nv) return null;
  const counter = await getCounter(uid, nv.name);
  if (!counter || counter.scope === 'channel') return null;
  const login = String(f.get('username') ?? '').trim().replace(/^@/, '').toLowerCase();
  const target = await resolveAddTarget(counter.scope, login, normalizeCounterName(f.get('command')));
  if (!target) return null;
  const found = await setCounter(uid, nv.name, nv.value, target);
  if (!found) throw new Error('unknown counter');
  return `${nv.name}[${target.viewerId || target.command}]=${nv.value}`;
}

export async function runDeleteEntry(uid: string, f: FormData): Promise<string | null> {
  const name = normalizeCounterName(f.get('name'));
  const target = bucketTarget(f);
  if (!name || !target) return null;
  if (!target.viewerId && !target.command) return null;
  const found = await deleteCounterEntry(uid, name, target);
  if (!found) throw new Error('unknown counter');
  return `${name}${bucketLabel(target)} removed`;
}
