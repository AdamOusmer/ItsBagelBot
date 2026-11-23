// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { invalidateAll } from '$app/navigation';
import type { ActionResult } from '$app/forms';
import type { I18n } from '@bagel/kit';
import { toast } from '@bagel/ui/svelte/toast';

export async function finishFreezeAction(result: ActionResult, releasing: boolean, t: I18n['t']): Promise<void> {
  if (result.type === 'success') {
    toast('success', t(releasing ? 'loyalty.toastUnfrozen' : 'loyalty.toastFrozen'));
  } else {
    toast('danger', t('loyalty.toastFreezeFailed'));
  }
  await invalidateAll();
}
