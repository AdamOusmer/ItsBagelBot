// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Assets } from './assets';

const stacks: Record<string, string> = {
  'System sans': 'system-ui, sans-serif',
  'System serif': 'Georgia, serif',
  'System mono': 'ui-monospace, monospace'
};
const loaded = new Set<string>();

const faceName = (family: string): string => `bb-${family.slice('asset:'.length)}`;

export function fontStack(family: string, assets: Assets): string {
  if (Object.hasOwn(stacks, family)) return stacks[family];
  const name = assets.resolve(family) ? faceName(family) : family.replace(/["\\]/g, '');
  return `"${name}", system-ui, sans-serif`;
}

export function loadFont(family: string, assets: Assets): void {
  const font = assets.resolve(family);
  if (font?.type !== 'woff2' || loaded.has(family)) return;
  loaded.add(family);
  const face = new FontFace(faceName(family), `url(${JSON.stringify(font.url)})`);
  document.fonts.add(face);
  void face.load().catch(() => undefined);
}
