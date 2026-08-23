// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

// Changelog: one JSON file per GitHub release. Drop in a new file to publish
// an entry — no page edits. `title` / `description` are either a plain English
// string or a locale map with `en` required; missing locales fall back to
// English. `tag` drives the stylized chips (alpha, beta, prerelease); `release`
// is the quiet stable mark. `version` is the git tag shown on the page (do not
// rely on the filename: the loader strips dots from ids).

const localized = z.union([
  z.string(),
  z.object({ en: z.string() }).catchall(z.string()),
]);

const changelog = defineCollection({
  loader: glob({ pattern: '*.json', base: './src/content/changelog' }),
  schema: z.object({
    tag: z.enum(['alpha', 'beta', 'prerelease', 'release']),
    version: z.string(),
    title: localized,
    description: localized,
    github: z.string().url(),
    date: z.coerce.date(),
  }),
});

export const collections = { legalMeta, legalSections, changelog };
