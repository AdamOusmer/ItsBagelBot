// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Screen dispatcher: ScreenName -> component. A screen is a still mock of one
// dashboard view, built from the @bagel/ui elements the console renders.
// Resolved from the directory so adding a screen is one new file: the name in
// ScreenName (types.ts) must equal the file's basename.
//
// The glob and the re-keying stay here rather than behind a shared helper: a
// helper cannot take the pattern as an argument (Vite rewrites `import.meta.glob`
// at build time and needs a literal), so all it could own is the two-line
// Object.fromEntries below, and owning it cost the map its element type, which
// GuideBody then had to cast away.
import type { GuideComponent, ScreenName } from '../../../lib/guides/types';

const modules = import.meta.glob<GuideComponent>('./*.astro', { eager: true, import: 'default' });

export const screens = Object.fromEntries(
    Object.entries(modules).map(([path, component]) => [
        path.slice('./'.length, -'.astro'.length),
        component,
    ]),
) as Record<ScreenName, GuideComponent | undefined>;
