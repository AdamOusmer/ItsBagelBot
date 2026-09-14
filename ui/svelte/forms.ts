// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { tick } from 'svelte';

/**
 * Move focus to the first field a editor has marked invalid.
 *
 * Validation messages are rendered reactively, so wait for Svelte to attach
 * the aria state before querying. Editors keep ownership of their rules and
 * copy; this helper makes the submit-time focus behavior consistent.
 */
export async function focusFirstInvalid(root: ParentNode | null): Promise<void> {
  await tick();
  root?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus();
}
