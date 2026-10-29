// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import manifest from '../../../../locales/manifest.json';
import { defaultLang } from './lang';

// Client code: importing ./ui would ship every locale's catalog with it.
const prefixes = new Set<string>(manifest.filter((code) => code !== defaultLang));

function routeOf(pathname: string): { lang: string; path: string } {
    const first = pathname.split('/')[1] ?? '';
    const lang = prefixes.has(first) ? first : defaultLang;
    const path = (lang === defaultLang ? pathname : pathname.slice(lang.length + 1)).replace(/\/+$/, '') || '/';
    return { lang, path };
}

let langSwitchScrollY: number | null = null;

function onBeforePreparation(event: Event): void {
    const { from, to } = event as Event & { from?: URL; to?: URL };
    if (!(from instanceof URL) || !(to instanceof URL)) {
        langSwitchScrollY = null;
        return;
    }

    const fromRoute = routeOf(from.pathname);
    const toRoute = routeOf(to.pathname);
    langSwitchScrollY =
        fromRoute.lang !== toRoute.lang && fromRoute.path === toRoute.path ? window.scrollY : null;
}

if (typeof document !== 'undefined') {
    document.addEventListener('astro:before-preparation', onBeforePreparation);
}

export function getLangSwitchScrollY(): number | null {
    return langSwitchScrollY;
}
