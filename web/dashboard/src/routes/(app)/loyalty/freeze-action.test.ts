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
const success = { type: 'success', status: 200, data: { ok: true } } as const;
const cases = [
  { releasing: false, result: unavailable, tone: 'danger', key: 'loyalty.toastFreezeFailed' },
  { releasing: true, result: unavailable, tone: 'danger', key: 'loyalty.toastFreezeFailed' },
  { releasing: false, result: success, tone: 'success', key: 'loyalty.toastFrozen' },
  { releasing: true, result: success, tone: 'success', key: 'loyalty.toastUnfrozen' }
] satisfies { releasing: boolean; result: ActionResult; tone: string; key: string }[];

test.each(cases)('refreshes after $key (unfreeze: $releasing)', async ({ result, releasing, tone, key }) => {
  invalidateAll.mockClear();
  toast.mockClear();
  await finishFreezeAction(result, releasing, (message) => message);
  expect({ refreshes: invalidateAll.mock.calls.length, toasts: toast.mock.calls }).toEqual({
    refreshes: 1,
    toasts: [[tone, key]]
  });
});
