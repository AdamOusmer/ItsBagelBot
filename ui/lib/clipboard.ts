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

/** Copy text, optionally falling back to selection-based copying on older browsers. */
export async function copyText(
  text: string,
  options: { legacyFallback?: boolean } = {}
): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    if (!options.legacyFallback || typeof document === 'undefined') return false;
  }

  const active = document.activeElement;
  const selection = document.getSelection();
  const ranges = selection
    ? Array.from({ length: selection.rangeCount }, (_, i) => selection.getRangeAt(i).cloneRange())
    : [];
  const field = document.createElement('textarea');
  field.value = text;
  field.style.cssText = 'position:fixed;top:0;left:-9999px;opacity:0';
  try {
    document.body.appendChild(field);
    field.select();
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    field.remove();
    if (active instanceof HTMLElement) active.focus({ preventScroll: true });
    if (selection) {
      selection.removeAllRanges();
      for (const range of ranges) selection.addRange(range);
    }
  }
}

/** Write text and pulse the caller's confirmation only after a successful copy. */
export async function copyFlash(
  text: string,
  flash: (on: boolean) => void,
  ms = FLASH_MS
): Promise<void> {
  if (!(await copyText(text))) {
    flash(false);
    return;
  }
  flash(true);
  setTimeout(() => flash(false), ms);
}
