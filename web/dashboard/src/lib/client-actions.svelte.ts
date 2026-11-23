// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { untrack } from 'svelte';
import { deserialize } from '$app/forms';
import { actionPayload, DISABLED_TRIGGERS_KEY, type ActionOk } from '@bagel/kit';
import type { SaveState } from '@bagel/ui/svelte/SaveStatus.svelte';

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

export function createTriggerToggle(read: () => string, onError: () => void) {
  let value = $state(untrack(read));
  let status = $state<SaveState>('idle');
  let timer: ReturnType<typeof setTimeout> | undefined;
  let chain: Promise<void> = Promise.resolve();

  reseedOnChange(read, (next) => {
    value = next;
  });

  function setStatus(next: SaveState, clearAfterMs?: number) {
    clearTimeout(timer);
    status = next;
    if (clearAfterMs) timer = setTimeout(() => { status = 'idle'; }, clearAfterMs);
  }

  function save(next: string) {
    const before = value;
    if (next === before) return;
    value = next;
    setStatus('saving');
    chain = chain.then(async () => {
      const body = new FormData();
      body.set(DISABLED_TRIGGERS_KEY, next);
      const payload = await postAction<ActionOk>('triggers', body);
      if (payload?.ok) {
        setStatus('saved', 3000);
        return;
      }
      if (value === next) value = before;
      setStatus('error', 4000);
      onError();
    });
  }

  return {
    get value() { return value; },
    get status() { return status; },
    save
  };
}
