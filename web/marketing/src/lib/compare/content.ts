// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { z } from 'astro/zod';
import type { CompareSlug } from './slugs';

const text = z.string().min(1);
const link = z.string().url();

const compareSchema = z.object({
  meta: z.object({ title: text.max(60), description: text.max(155) }),
  hero: z.object({ eyebrow: text, title: text, lead: text }),
  reviewed: z.string().regex(/^\d{4}-\d{2}-\d{2}$/),
  suits: z.object({ us: z.array(text).min(3).max(4), them: z.array(text).min(3).max(4) }),
  rows: z.array(z.object({ feature: text, us: text, them: text, source: link })).min(1),
  migrate: z.object({ title: text, steps: z.array(text).min(3).max(4), cta: text }),
  faq: z.array(z.object({ q: text, a: text })).min(1),
  sources: z.array(z.object({ label: text, url: link })).min(1),
});

export type CompareContent = z.infer<typeof compareSchema>;

const files = import.meta.glob<unknown>('../../content/compare/*.json', { eager: true, import: 'default' });

export function getCompare(slug: CompareSlug, lang: string): CompareContent {
  return compareSchema.parse(files[`../../content/compare/${slug}.${lang}.json`]);
}
