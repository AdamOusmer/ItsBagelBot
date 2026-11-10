// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

function isEdgeInjected(script: HTMLScriptElement): boolean {
    return (script.getAttribute('src') ?? script.text).includes('/cdn-cgi/');
}

// The edge appends per-response bootstraps to every HTML body; running one twice in a document fails its challenge calls.
document.addEventListener('astro:before-swap', (event) => {
    event.newDocument
        .querySelectorAll<HTMLScriptElement>('script:not([type*="json"])')
        .forEach((script) => {
            if (isEdgeInjected(script)) script.remove();
        });
});
