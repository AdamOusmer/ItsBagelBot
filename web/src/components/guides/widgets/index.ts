// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Widget dispatcher: WidgetName -> component. A widget is a block that does
// something in the browser; a screen is a still picture of the dashboard.
// Resolved from the directory so adding a widget is one new file: the name in
// WidgetName (types.ts) must equal the file's basename. A name with no file
// fails at build time in GuideBody.
import type { WidgetName } from '../../../lib/guides/types';
import { globDir } from '../../../lib/guides/dispatcher';

export const widgets = globDir<WidgetName>(
    import.meta.glob('./*.astro', { eager: true, import: 'default' }),
);
