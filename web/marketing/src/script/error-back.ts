// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The "go back" control on the 404 and 500 pages.
//
// Its own module, and not an inline <script> in each page, because the error
// SCENE is @bagel/ui's now (astro/ErrorScene.astro) and the scene deliberately
// carries no behaviour: it draws the orbits, the outline code and the action
// row, and every sentence and every action arrives from the app. This is the
// marketing site's half. The console's half is the same shape and lives in
// web/kit/components/ErrorView.svelte.
//
// Delegated from the document rather than bound to the button, because the
// site runs Astro's ClientRouter: a handler bound on load is lost the first
// time a visitor navigates away from the error page and back to it.
//
// The home fallback comes off the button's own data attribute so it lands in
// the locale the visitor was reading, not always at the English root.
export function mountErrorBack(): void {
    document.addEventListener('click', (event) => {
        const target = event.target;
        if (!(target instanceof Element)) return;
        const back = target.closest<HTMLElement>('[data-error-back]');
        if (!back) return;
        if (window.history.length > 1) window.history.back();
        else window.location.assign(back.dataset.errorBack || '/');
    });
}
