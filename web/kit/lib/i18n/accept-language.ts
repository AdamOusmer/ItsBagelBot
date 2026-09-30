// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

type Weighted = { range: string; q: number; index: number };

function weigh(part: string, index: number): Weighted {
  const [tag, ...params] = part.split(';').map((p) => p.trim());
  const q = params.find((p) => p.startsWith('q='));
  return { range: tag.toLowerCase().replaceAll('_', '-'), q: q ? Number(q.slice(2)) : 1, index };
}

// Cloudflare's Vary normalization keys the edge cache by q-weight order, so the
// origin must pick the language the same way or a cached variant can mismatch.
export function acceptedLanguages(header: string | null | undefined): string[] {
  return (header ?? '')
    .split(',')
    .map(weigh)
    .filter((l) => l.range !== '' && l.q > 0)
    .sort((a, b) => b.q - a.q || a.index - b.index)
    .map((l) => l.range);
}

/** The first of `locales` the header accepts: exact tag, then its base language, then a region of that language. */
export function matchAcceptLanguage(header: string | null | undefined, locales: readonly string[]): string | undefined {
  for (const range of acceptedLanguages(header)) {
    const base = range.split('-')[0];
    const match =
      locales.find((locale) => locale === range) ??
      locales.find((locale) => locale === base) ??
      locales.find((locale) => locale.startsWith(`${base}-`));
    if (match) return match;
  }
  return undefined;
}
