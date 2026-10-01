// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { splitLocale } from './ui';

let langSwitchScrollY: number | null = null;

function onBeforePreparation(event: Event): void {
    const { from, to } = event as Event & { from?: URL; to?: URL };
    if (!(from instanceof URL) || !(to instanceof URL)) {
        langSwitchScrollY = null;
        return;
    }

    const fromRoute = splitLocale(from.pathname);
    const toRoute = splitLocale(to.pathname);
    langSwitchScrollY =
        fromRoute.lang !== toRoute.lang && fromRoute.path === toRoute.path ? window.scrollY : null;
}

if (typeof document !== 'undefined') {
    document.addEventListener('astro:before-preparation', onBeforePreparation);
}

export function getLangSwitchScrollY(): number | null {
    return langSwitchScrollY;
}
