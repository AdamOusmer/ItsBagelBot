// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const DEFAULT_SELECTOR = 'main > header, main > section, main > div > section, main > .seam, footer';

export function watchOffstage(root: ParentNode = document, selector = DEFAULT_SELECTOR): () => void {
    if (typeof IntersectionObserver === 'undefined') return () => {};

    const targets = Array.from(root.querySelectorAll<HTMLElement>(selector));
    if (!targets.some((el) => typeof el.getAnimations === 'function')) return () => {};

    const ours = new WeakSet<Animation>();
    const paused = new Map<Element, Animation[]>();

    const resume = (el: Element) => {
        for (const animation of paused.get(el) ?? []) {
            if (ours.delete(animation)) animation.play();
        }
        paused.delete(el);
    };

    const suspend = (el: Element) => {
        resume(el);
        if (typeof el.getAnimations !== 'function') return;
        const timeline = el.ownerDocument.timeline;
        const list: Animation[] = [];
        for (const animation of el.getAnimations({ subtree: true })) {
            if (animation.timeline !== timeline) continue;
            if (animation.effect?.getTiming().iterations !== Infinity) continue;
            if (animation.playState !== 'running') continue;
            animation.pause();
            ours.add(animation);
            list.push(animation);
        }
        paused.set(el, list);
    };

    const observer = new IntersectionObserver(
        (entries) => {
            for (const entry of entries) {
                if (entry.isIntersecting) resume(entry.target);
                else suspend(entry.target);
            }
        },
        { rootMargin: '25% 0px 25% 0px' },
    );
    for (const el of targets) observer.observe(el);

    return () => {
        observer.disconnect();
        for (const el of Array.from(paused.keys())) resume(el);
    };
}
