// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import type { ActionResult } from '$app/forms';
import { stubSvelteKit } from '../../../../test/sveltekit';

const invalidateAll = mock(async () => {});
const toast = mock();
stubSvelteKit({ invalidateAll });
mock.module('@bagel/ui/svelte/toast', () => ({ toast }));

const { finishFreezeAction } = await import('./freeze-action');

const unavailable = { type: 'failure', status: 502, data: { ok: false }, location: '/loyalty' } as const;
const heldByRound = { type: 'failure', status: 409, data: { reason: 'held_by_round' }, location: '/loyalty' } as const;
const cases = [
  { releasing: false, result: unavailable, key: 'loyalty.toastFreezeFailed' },
  { releasing: true, result: unavailable, key: 'loyalty.toastFreezeFailed' },
  { releasing: true, result: heldByRound, key: 'loyalty.toastUnfreezeRound' }
] satisfies { releasing: boolean; result: ActionResult; key: string }[];

test.each(cases)('refreshes after $key (unfreeze: $releasing)', async ({ result, releasing, key }) => {
  invalidateAll.mockClear();
  toast.mockClear();
  await finishFreezeAction(result, releasing, (message) => message);
  expect({ refreshes: invalidateAll.mock.calls.length, toasts: toast.mock.calls }).toEqual({
    refreshes: 1,
    toasts: [['danger', key]]
  });
});
