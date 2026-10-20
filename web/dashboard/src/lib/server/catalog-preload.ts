// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { DEFAULT_LOCALE } from '@bagel/kit/i18n';
import { sliced, urls } from 'virtual:bagel-i18n';

const CHARSET_META = '<meta charset="utf-8" />';
const LINKED_CHUNK = /<\/_app\/immutable\/[\w-]+\/([\w.-]+)\.js>/g;
// Same build directory serve-node.js serves the client from; the client build writes this list after the server build.
const SLICED_CHUNKS = resolve(process.env.SVELTEKIT_BUILD_DIR || 'build', 'client/_app/i18n-slices.json');

let slicedChunks: ReadonlySet<string> | undefined;

function readSlicedChunks(): ReadonlySet<string> {
  try {
    return new Set(JSON.parse(readFileSync(SLICED_CHUNKS, 'utf8')) as string[]);
  } catch {
    return new Set();
  }
}

export function preloadCatalog(html: string, locale: string): string {
  const href = sliced ? undefined : urls[locale];
  if (!href || !html.includes(CHARSET_META)) return html;
  return html.replace(CHARSET_META, `${CHARSET_META}<link rel="preload" as="fetch" crossorigin="anonymous" href="${href}" />`);
}

export function preloadSlices(headers: Headers, locale: string): void {
  const link = sliced && locale !== DEFAULT_LOCALE ? headers.get('link') : null;
  if (!link) return;
  slicedChunks ??= readSlicedChunks();
  const chunks = new Set([...link.matchAll(LINKED_CHUNK)].map((match) => match[1]).filter((chunk) => slicedChunks!.has(chunk)));
  const slices = [...chunks].map((chunk) => `</_app/immutable/i18n/${locale}/${chunk}.json>; rel="preload"; as="fetch"; crossorigin="anonymous"; nopush`);
  if (slices.length) headers.set('link', [link, ...slices].join(', '));
}
