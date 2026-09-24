// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, it } from 'bun:test';
import { bestEffort } from './best-effort';

describe('bestEffort', () => {
  it('passes a resolved value through untouched', async () => {
    await expect(bestEffort(Promise.resolve({ n: 1 }), { n: 0 })).resolves.toEqual({ n: 1 });
  });

  it('degrades to the fallback on a rejection', async () => {
    await expect(bestEffort(Promise.reject(new Error('rpc down')), { n: 0 })).resolves.toEqual({
      n: 0
    });
  });

  it('keeps a falsy resolved value rather than substituting the fallback', async () => {
    await expect(bestEffort(Promise.resolve(0), 7)).resolves.toBe(0);
    await expect(bestEffort(Promise.resolve(null), 'x')).resolves.toBeNull();
  });
});
