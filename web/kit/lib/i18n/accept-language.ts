// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

type Weighted = { base: string; q: number; index: number };

function weigh(part: string, index: number): Weighted {
  const [tag, ...params] = part.split(';').map((p) => p.trim());
  const q = params.find((p) => p.startsWith('q='));
  return { base: tag.toLowerCase().split('-')[0], q: q ? Number(q.slice(2)) : 1, index };
}

// Cloudflare's Vary normalization keys the edge cache by q-weight order, so the
// origin must pick the language the same way or a cached variant can mismatch.
export function acceptedLanguages(header: string | null | undefined): string[] {
  return (header ?? '')
    .split(',')
    .map(weigh)
    .filter((l) => l.base !== '' && l.q > 0)
    .sort((a, b) => b.q - a.q || a.index - b.index)
    .map((l) => l.base);
}
