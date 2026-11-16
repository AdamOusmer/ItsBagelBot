// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { z } from 'astro/zod';
import { compareSlugs } from './slugs';

const text = z.string().min(1);
const link = z.string().url();

const roundupSchema = z.object({
  meta: z.object({ title: text.max(60), description: text.max(155) }),
  hero: z.object({ eyebrow: text, title: text, lead: text }),
  reviewed: z.string().regex(/^\d{4}-\d{2}-\d{2}$/),
  table: z.object({
    eyebrow: text,
    title: text,
    columns: z.object({ bot: text, music: text, plays: text, price: text, source: text }),
    sourceLink: text,
    rows: z.array(z.object({
      id: z.enum(['itsbagelbot', ...compareSlugs]),
      music: text,
      plays: text,
      price: text,
      source: link,
    })).min(1),
  }),
  picks: z.object({
    eyebrow: text,
    title: text,
    items: z.array(z.object({ name: text, desc: text })).min(1),
  }),
  faqTitle: text,
  faq: z.array(z.object({ q: text, a: text })).min(1),
  sources: z.array(z.object({ label: text, url: link })).min(1),
});

export type RoundupContent = z.infer<typeof roundupSchema>;

export const roundupPath = '/vs/song-request-bots';

const files = import.meta.glob<unknown>('../../content/roundup/*.json', { eager: true, import: 'default' });

export function getRoundup(lang: string): RoundupContent {
  return roundupSchema.parse(files[`../../content/roundup/song-request-bots.${lang}.json`]);
}
