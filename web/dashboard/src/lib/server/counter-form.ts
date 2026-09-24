// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { CounterScope } from '@bagel/kit';
import { normalizeCounterName, parseCounterValue } from '@bagel/kit/validation';
import { resolveViewerId, type CounterTarget } from './loyalty-store';

export class UserError extends Error {}

export { normalizeCounterName };

export function namedValue(f: FormData): { name: string; value: string } | null {
  const name = normalizeCounterName(f.get('name'));
  const value = parseCounterValue(f.get('value'));
  if (!name || value === null) return null;
  return { name, value };
}

export function bucketTarget(f: FormData): CounterTarget | null {
  const viewerId = String(f.get('viewer_id') ?? '').trim();
  if (viewerId && !/^\d+$/.test(viewerId)) return null;
  return { viewerId, command: normalizeCounterName(f.get('command')) };
}

export function bucketLabel(t: CounterTarget): string {
  if (!t.viewerId && !t.command) return '';
  return `[${t.viewerId}${t.command ? ':' + t.command : ''}]`;
}

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
