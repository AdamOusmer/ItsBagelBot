// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { tick } from 'svelte';

export async function focusFirstInvalid(root: ParentNode | null): Promise<void> {
  await tick();
  root?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
}
