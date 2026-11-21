// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { getAbortSignal } from 'svelte';

export function applyStreamed<T>(pending: Promise<T>, apply: (value: T) => void): void {
  const signal = getAbortSignal();
  void pending.then((value) => {
    if (!signal.aborted) apply(value);
  });
}
