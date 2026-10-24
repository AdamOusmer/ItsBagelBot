// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { untrack } from 'svelte';
import { deserialize } from '$app/forms';
import { actionPayload, type ActionOk } from '@bagel/kit';

export async function postAction<T = ActionOk>(action: string, body: FormData, path = ''): Promise<T | null> {
  try {
    const res = await fetch(`${path}?/${action}`, { method: 'POST', body });
    return actionPayload<T>(deserialize(await res.text())) ?? null;
  } catch {
    return null;
  }
}

export function reseedOnChange<T>(read: () => T, apply: (next: T) => void): void {
  let seed = untrack(read);
  $effect(() => {
    const next = read();
    if (next !== seed) {
      seed = next;
      apply(next);
    }
  });
}
