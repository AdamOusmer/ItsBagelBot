// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const DEFAULT_SELECTOR = 'main > header, main > section, main > div > section, main > .seam, footer';

type Registry = {
    ours: WeakSet<Animation>;
    paused: Map<Element, Animation[]>;
};

function isPausable(animation: Animation, timeline: AnimationTimeline): boolean {
    return (
        animation.timeline === timeline &&
        animation.effect?.getTiming().iterations === Infinity &&
        animation.playState === 'running'
    );
}

function pausableAnimations(el: Element): Animation[] {
    if (typeof el.getAnimations !== 'function') return [];
    const timeline = el.ownerDocument.timeline;
    return el.getAnimations({ subtree: true }).filter((animation) => isPausable(animation, timeline));
}

function playTarget(registry: Registry, el: Element): void {
    for (const animation of registry.paused.get(el) ?? []) {
        if (registry.ours.delete(animation)) animation.play();
    }
    registry.paused.delete(el);
}

function pauseTarget(registry: Registry, el: Element): void {
    playTarget(registry, el);
    const list = pausableAnimations(el);
    for (const animation of list) {
        animation.pause();
        registry.ours.add(animation);
    }
    registry.paused.set(el, list);
}

function onIntersections(registry: Registry, entries: IntersectionObserverEntry[]): void {
    for (const entry of entries) {
        if (entry.isIntersecting) playTarget(registry, entry.target);
        else pauseTarget(registry, entry.target);
    }
}

export function watchOffstage(root: ParentNode = document, selector = DEFAULT_SELECTOR): () => void {
    if (typeof IntersectionObserver === 'undefined') return () => {};

    const targets = Array.from(root.querySelectorAll<HTMLElement>(selector));
    if (!targets.some((el) => typeof el.getAnimations === 'function')) return () => {};

    const registry: Registry = { ours: new WeakSet(), paused: new Map() };
    const observer = new IntersectionObserver((entries) => onIntersections(registry, entries), {
        rootMargin: '25% 0px 25% 0px',
    });
    for (const el of targets) observer.observe(el);

    return () => {
        observer.disconnect();
        for (const el of Array.from(registry.paused.keys())) playTarget(registry, el);
    };
}
