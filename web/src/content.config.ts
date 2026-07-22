import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';

// Legal documents (terms, privacy, creator-terms) as pure data: one folder per
// doc, one subfolder per locale, a meta.json plus NN-<anchor>.md per section.
// The NN- prefix orders sections; the rest of the filename is the anchor id
// (must stay stable — it is the in-page #hash). A translator adds a language by
// copying a locale folder and translating the files; no code changes.

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

export const collections = { legalMeta, legalSections };
