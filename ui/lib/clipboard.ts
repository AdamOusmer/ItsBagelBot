// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const FLASH_MS = 1600;

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
