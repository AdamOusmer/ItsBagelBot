// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Copy-to-clipboard with the "Copied" flash, shared by every surface that has
// a copy button: both consoles and the marketing site's `[data-copy]` chips.
//
// The flash flag stays in the caller's `$state` (a plain module cannot hold
// one, and a shared store would let two buttons on the same page flash
// together); what is shared is the part every copy got right by accident and
// could get wrong: a rejected clipboard write must clear the flag rather than
// leave the button claiming a copy that never happened.

/**
 * How long the "Copied" state stays up.
 *
 * There were two numbers: 1600 in the marketing site's `[data-copy]` handler
 * (script/interactions.js) and 1500 here, from the console. Neither was ever
 * measured against the other; they were written months apart, which is how a
 * 100ms difference nobody can see becomes something two surfaces disagree on.
 *
 * 1600 wins, and not by coin toss. Taking the LONGER of the two fails safe: a
 * flash still up when you look back at it is unremarkable, one that has
 * already gone leaves you unsure the click landed. It is also the value that
 * was tuned against the harder case — the marketing chips copy command strings
 * the visitor reads back to check they got the right one, where the console's
 * copy targets are short and sit under the cursor. The console's confirmation
 * gets 100ms longer and nothing else about it changes.
 */
const FLASH_MS = 1600;

/**
 * Write `text` to the clipboard and pulse `flash` true for ~1.6s. On failure
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
