// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { CommandView } from './types';
import { parseCounterValue } from './validation';

export function usesCount(c: Pick<CommandView, 'uses'>): bigint {
  if (c.uses == null) return 0n;
  const value = parseCounterValue(c.uses);
  if (value === null) throw new RangeError('Invalid command use count');
  return BigInt(value);
}

export function compareUses(a: Pick<CommandView, 'uses'>, b: Pick<CommandView, 'uses'>): number {
  const left = usesCount(a);
  const right = usesCount(b);
  return left < right ? -1 : left > right ? 1 : 0;
}
