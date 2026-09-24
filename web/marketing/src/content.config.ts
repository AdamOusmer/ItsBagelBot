// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';

const legalMeta = defineCollection({
  loader: glob({ pattern: '*/*/meta.json', base: './src/content/legal' }),
  schema: z.object({
    metaTitle: z.string(),
    metaDescription: z.string(),
    eyebrow: z.string(),
    title: z.string(),
    description: z.string(),
    updated: z.string(),
  }),
});

const legalSections = defineCollection({
  loader: glob({ pattern: '*/*/[0-9][0-9]-*.md', base: './src/content/legal' }),
  schema: z.object({
    heading: z.string(),
    plain: z.string(),
  }),
});

const localized = z.union([
  z.string(),
  z.object({ en: z.string() }).catchall(z.string()),
]);

const localizedList = z.union([
  z.array(z.string()).min(1),
  z.object({ en: z.array(z.string()).min(1) }).catchall(z.array(z.string())),
]);

const changelog = defineCollection({
  loader: glob({ pattern: '*.json', base: './src/content/changelog' }),
  schema: z.object({
    tag: z.enum(['alpha', 'beta', 'prerelease', 'release']),
    version: z.string(),
    title: localized,
    highlights: localizedList,
    github: z.string().url(),
    date: z.coerce.date(),
  }),
});

export const collections = { legalMeta, legalSections, changelog };
