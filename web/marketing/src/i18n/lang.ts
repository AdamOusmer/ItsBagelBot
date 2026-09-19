// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Glob-free split of ui.ts: the two primitives (Lang, defaultLang) that
// builder.ts and lib/variables/index.ts need but that must not drag in
// ui.ts's import.meta.glob('./locales/*.json'), which only Vite/Astro can
// run. bun test evaluates builder.ts directly (lib/variables/index.ts ->
// builder.ts), and bun does not implement import.meta.glob, so anything on
// that import path has to bottom out here instead of in ui.ts. ui.ts
// re-exports both names so every existing importer of ui.ts is unaffected.

/** A locale code (e.g. 'en', 'fr'). Open set: whatever JSON files exist. */
export type Lang = string;

export const defaultLang: Lang = 'en';
