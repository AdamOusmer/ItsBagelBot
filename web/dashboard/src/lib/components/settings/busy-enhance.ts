// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { SubmitFunction } from '@sveltejs/kit';

export function busyEnhance(
  setBusy: (busy: boolean) => void,
  onDone: (resultType: string) => void
): SubmitFunction {
  return () => {
    setBusy(true);
    return async ({ result, update }) => {
      await update();
      setBusy(false);
      onDone(result.type);
    };
  };
}
