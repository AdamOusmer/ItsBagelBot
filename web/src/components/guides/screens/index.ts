// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Screen dispatcher: ScreenName -> component. A screen is a still mock of one
// dashboard view, built from the df-* classes in src/styles/dashframe.css.
// Resolved from the directory so adding a screen is one new file: the name in
// ScreenName (types.ts) must equal the file's basename.
import type { ScreenName } from '../../../lib/guides/types';
import { globDir } from '../../../lib/guides/dispatcher';

export const screens = globDir<ScreenName>(
    import.meta.glob('./*.astro', { eager: true, import: 'default' }),
);
