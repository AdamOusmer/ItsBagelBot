// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Copy-to-clipboard with the "Copied" flash, shared by both consoles.
//
// The flash flag stays in the caller's `$state` (a plain module cannot hold
// one, and a shared store would let two buttons on the same page flash
// together); what is shared is the part every copy got right by accident and
// could get wrong: a rejected clipboard write must clear the flag rather than
// leave the button claiming a copy that never happened.

const FLASH_MS = 1500;

/**
 * Write `text` to the clipboard and pulse `flash` true for ~1.5s. On failure
 * (permission denied, insecure context) `flash` is set false and nothing is
 * thrown: a copy button has no error state worth raising a toast for.
 */
export async function copyFlash(
  text: string,
  flash: (on: boolean) => void,
  ms = FLASH_MS
): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    flash(true);
    setTimeout(() => flash(false), ms);
  } catch {
    flash(false);
  }
}
