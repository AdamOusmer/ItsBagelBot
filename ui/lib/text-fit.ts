// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type FitAxis = 'x' | 'y';

type FitTarget = {
    el: HTMLElement;
    box: HTMLElement;
    block: boolean;
    group: string | undefined;
    tracking: number;
};

const SELECTOR = '[data-fit]';
const BOX_SELECTOR = '[data-fit-box]';
const PROPERTY = '--bb-fit';
const FLOOR = 0.5;
const LINE_SAFETY = 0.99;
// Rounding can leave a pixel after the linear estimate, so the pass repeats until the label fits.
const LINE_PASSES = 8;
const BLOCK_STEPS = 6;

// Scroll metrics miss overflow into padding and count glyph ink past tight line boxes, so compare edges.
export function measureDeficit(el: HTMLElement, box: HTMLElement, axis: FitAxis): number {
    const style = getComputedStyle(box);
    const outer = box.getBoundingClientRect();
    const inner = el.getBoundingClientRect();
    if (axis === 'x') {
        const left = outer.left + parseFloat(style.borderLeftWidth) + parseFloat(style.paddingLeft);
        const right = outer.right - parseFloat(style.borderRightWidth) - parseFloat(style.paddingRight);
        const spill = Math.max(0, left - inner.left) + Math.max(0, inner.right - right);
        return Math.round(spill + (el.scrollWidth - el.clientWidth));
    }
    const bottom = outer.bottom - parseFloat(style.borderBottomWidth) - parseFloat(style.paddingBottom);
    return Math.round(inner.bottom - bottom);
}

export function fitRatio({ needed, deficit }: { needed: number; deficit: number }): number {
    if (deficit <= 0 || needed <= 0) return 1;
    return Math.max(FLOOR, Math.min(1, ((needed - deficit) / needed) * LINE_SAFETY));
}

export function groupMin(items: readonly { group: string | undefined; ratio: number }[]): number[] {
    const smallest = new Map<string, number>();
    for (const { group, ratio } of items) {
        if (group !== undefined) smallest.set(group, Math.min(ratio, smallest.get(group) ?? 1));
    }
    return items.map(({ group, ratio }) => (group === undefined ? ratio : smallest.get(group) ?? ratio));
}

// Inherited letter-spacing computes to px, so it is scaled here or it would not shrink with the glyphs.
function setRatio({ el, tracking }: FitTarget, ratio: number): void {
    if (ratio >= 1) {
        el.style.removeProperty(PROPERTY);
        el.style.removeProperty('letter-spacing');
        return;
    }
    el.style.setProperty(PROPERTY, String(ratio));
    if (tracking) el.style.letterSpacing = `${tracking * ratio}px`;
}

function fits(el: HTMLElement, box: HTMLElement): boolean {
    return measureDeficit(el, box, 'y') <= 0 && measureDeficit(el, box, 'x') <= 0;
}

function lineRatio(target: FitTarget): number {
    const { el, box } = target;
    let ratio = 1;
    for (let pass = 0; pass < LINE_PASSES && ratio > FLOOR; pass++) {
        const deficit = measureDeficit(el, box, 'x');
        if (deficit <= 0) break;
        ratio = Math.max(FLOOR, ratio * fitRatio({ needed: el.getBoundingClientRect().width, deficit }));
        setRatio(target, ratio);
    }
    return ratio;
}

function blockRatio(target: FitTarget): number {
    const { el, box } = target;
    if (fits(el, box)) return 1;
    let low = FLOOR;
    let high = 1;
    for (let step = 0; step < BLOCK_STEPS; step++) {
        const mid = (low + high) / 2;
        setRatio(target, mid);
        if (fits(el, box)) low = mid;
        else high = mid;
    }
    return low;
}

function fitAll(targets: readonly FitTarget[]): void {
    for (const target of targets) setRatio(target, 1);
    const ratios = targets.map((target) => {
        target.tracking = parseFloat(getComputedStyle(target.el).letterSpacing) || 0;
        const ratio = target.block ? blockRatio(target) : lineRatio(target);
        setRatio(target, ratio);
        return { group: target.group, ratio };
    });
    groupMin(ratios).forEach((ratio, i) => {
        setRatio(targets[i], ratio);
        targets[i].el.dataset.fitReady = '';
    });
}

function targetOf(el: HTMLElement): FitTarget | null {
    const box = el.parentElement?.closest<HTMLElement>(BOX_SELECTOR) ?? el.parentElement;
    if (!box) return null;
    return { el, box, block: el.dataset.fit === 'block', group: el.dataset.fitGroup || undefined, tracking: 0 };
}

function collect(root: ParentNode): FitTarget[] {
    const found = Array.from(root.querySelectorAll<HTMLElement>(SELECTOR));
    if (root instanceof HTMLElement && root.matches(SELECTOR)) found.unshift(root);
    return found.map(targetOf).filter((target): target is FitTarget => target !== null);
}

type FitRegistry = {
    targets: Map<HTMLElement, FitTarget>;
    boxes: Map<HTMLElement, Set<FitTarget>>;
    resizer: ResizeObserver;
    active: boolean;
};

function addedTargets(records: MutationRecord[], registry: FitRegistry): FitTarget[] {
    const added = new Map<HTMLElement, FitTarget>();
    for (const record of records) {
        for (const node of record.addedNodes) {
            if (!(node instanceof HTMLElement)) continue;
            for (const target of collect(node)) {
                if (!registry.targets.has(target.el)) added.set(target.el, target);
            }
        }
    }
    return Array.from(added.values());
}

function touchedTargets(records: MutationRecord[], registry: FitRegistry): FitTarget[] {
    const touched = new Set<FitTarget>();
    for (const record of records) {
        const el = (record.target as Element).closest?.(SELECTOR);
        const target = el ? registry.targets.get(el as HTMLElement) : undefined;
        if (target) touched.add(target);
    }
    return Array.from(touched);
}

// Group members share one ratio, so refitting a member alone would drop the smallest of its group.
function withGroupPeers(registry: FitRegistry, subset: FitTarget[]): FitTarget[] {
    const groups = new Set(subset.map((target) => target.group).filter((group) => group !== undefined));
    if (groups.size === 0) return subset;
    const refit = new Set(subset);
    for (const target of registry.targets.values()) {
        if (target.group !== undefined && groups.has(target.group)) refit.add(target);
    }
    return Array.from(refit);
}

function register(registry: FitRegistry, added: FitTarget[]): void {
    for (const target of added) {
        registry.targets.set(target.el, target);
        const siblings = registry.boxes.get(target.box);
        if (siblings) siblings.add(target);
        else {
            registry.boxes.set(target.box, new Set([target]));
            registry.resizer.observe(target.box);
        }
    }
}

function prune(registry: FitRegistry): void {
    for (const [el, target] of registry.targets) {
        if (el.isConnected) continue;
        registry.targets.delete(el);
        const siblings = registry.boxes.get(target.box);
        siblings?.delete(target);
        if (siblings?.size) continue;
        registry.boxes.delete(target.box);
        registry.resizer.unobserve(target.box);
    }
}

function refit(registry: FitRegistry, subset: FitTarget[]): void {
    if (subset.length) fitAll(withGroupPeers(registry, subset));
}

function boxTargets(registry: FitRegistry, entries: ResizeObserverEntry[]): FitTarget[] {
    return entries.flatMap((entry) => Array.from(registry.boxes.get(entry.target as HTMLElement) ?? []));
}

export function mountTextFit(root: ParentNode = document): () => void {
    const registry: FitRegistry = {
        targets: new Map(),
        boxes: new Map(),
        resizer: new ResizeObserver((entries) => {
            if (!registry.active) return;
            prune(registry);
            refit(registry, boxTargets(registry, entries));
        }),
        active: true,
    };
    const refitAll = () => {
        if (!registry.active) return;
        prune(registry);
        fitAll(Array.from(registry.targets.values()));
    };
    const onMutations = (records: MutationRecord[]) => {
        if (!registry.active) return;
        prune(registry);
        refit(registry, touchedTargets(records, registry));
        const added = addedTargets(records, registry);
        register(registry, added);
        refit(registry, added);
    };

    const initial = collect(root);
    register(registry, initial);
    fitAll(initial);
    const adder = new MutationObserver(onMutations);
    adder.observe(root, { childList: true, subtree: true });
    void document.fonts.ready.then(refitAll);
    document.fonts.addEventListener('loadingdone', refitAll);

    return () => {
        registry.active = false;
        registry.resizer.disconnect();
        adder.disconnect();
        registry.targets.clear();
        registry.boxes.clear();
        document.fonts.removeEventListener('loadingdone', refitAll);
    };
}
