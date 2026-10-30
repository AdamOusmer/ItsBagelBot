// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from './motion-query';

// A closed <details> stops rendering its body at once, so CSS alone can only
// animate the first open: every close snaps shut and the reopen after it has
// no rendered 0fr frame to start from. data-motion carries the visual state
// instead, and the element stays [open] until its collapse has played out.
export const DISCLOSURE_MOTION = 'data-motion';

const BODY = ':scope > .bb-disclosure__body';
const SUMMARY = ':scope > summary';
const SETTLE_FALLBACK_MS = 600;

type State = 'open' | 'closed';

export function wireDisclosure(details: HTMLDetailsElement): () => void {
    const body = details.querySelector<HTMLElement>(BODY);
    const summary = details.querySelector<HTMLElement>(SUMMARY);
    if (!body || !summary) return () => {};

    let settle: (() => void) | null = null;
    const state = (): State => (details.getAttribute(DISCLOSURE_MOTION) === 'open' ? 'open' : 'closed');
    const set = (next: State) => details.setAttribute(DISCLOSURE_MOTION, next);

    const cancelSettle = () => {
        settle?.();
        settle = null;
    };

    const expand = () => {
        cancelSettle();
        if (!details.open) details.open = true;
        // Commit the collapsed frame so grid-template-rows has a start value.
        void body.offsetHeight;
        set('open');
    };

    const collapse = () => {
        cancelSettle();
        set('closed');
        if (prefersReducedMotion()) {
            details.open = false;
            return;
        }
        const finish = () => {
            cancelSettle();
            if (state() === 'closed') details.open = false;
        };
        const onEnd = (event: TransitionEvent) => {
            if (event.target === body && event.propertyName === 'grid-template-rows') finish();
        };
        const timer = setTimeout(finish, SETTLE_FALLBACK_MS);
        body.addEventListener('transitionend', onEnd);
        settle = () => {
            clearTimeout(timer);
            body.removeEventListener('transitionend', onEnd);
        };
    };

    const onClick = (event: MouseEvent) => {
        event.preventDefault();
        if (state() === 'open') collapse();
        else expand();
    };

    // Opens and closes that bypass the summary: a bound `open` prop, an
    // exclusive `name` group, find-in-page.
    const onToggle = () => {
        if (details.open && state() !== 'open') expand();
        else if (!details.open && state() !== 'closed') {
            cancelSettle();
            set('closed');
        }
    };

    set(details.open ? 'open' : 'closed');
    summary.addEventListener('click', onClick);
    details.addEventListener('toggle', onToggle);
    return () => {
        cancelSettle();
        summary.removeEventListener('click', onClick);
        details.removeEventListener('toggle', onToggle);
        details.removeAttribute(DISCLOSURE_MOTION);
    };
}
