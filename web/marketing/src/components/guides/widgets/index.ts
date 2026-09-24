// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideComponent, WidgetName } from '../../../lib/guides/types';

const modules = import.meta.glob<GuideComponent>('./*.astro', { eager: true, import: 'default' });

export const widgets = Object.fromEntries(
    Object.entries(modules).map(([path, component]) => [
        path.slice('./'.length, -'.astro'.length),
        component,
    ]),
) as Record<WidgetName, GuideComponent | undefined>;
