// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type FitAxis = 'x' | 'y';

type FitTarget = {
    el: HTMLElement;
    box: HTMLElement;
    block: boolean;
    group: string | undefined;
};

const SELECTOR = '[data-fit]';
const BOX_SELECTOR = '[data-fit-box]';
const PROPERTY = '--bb-fit';
const FLOOR = 0.5;
const LINE_SAFETY = 0.99;
// Inherited px letter-spacing does not scale with the ratio, so one linear pass can still overflow.
const LINE_PASSES = 3;
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

function setRatio(el: HTMLElement, ratio: number): void {
    if (ratio >= 1) el.style.removeProperty(PROPERTY);
    else el.style.setProperty(PROPERTY, String(ratio));
}

function fits(el: HTMLElement, box: HTMLElement): boolean {
    return measureDeficit(el, box, 'y') <= 0 && measureDeficit(el, box, 'x') <= 0;
}

function lineRatio({ el, box }: FitTarget): number {
    let ratio = 1;
    for (let pass = 0; pass < LINE_PASSES && ratio > FLOOR; pass++) {
        const deficit = measureDeficit(el, box, 'x');
        if (deficit <= 0) break;
        ratio = Math.max(FLOOR, ratio * fitRatio({ needed: el.getBoundingClientRect().width, deficit }));
        setRatio(el, ratio);
    }
    return ratio;
}

function blockRatio({ el, box }: FitTarget): number {
    if (fits(el, box)) return 1;
    let low = FLOOR;
    let high = 1;
    for (let step = 0; step < BLOCK_STEPS; step++) {
        const mid = (low + high) / 2;
        setRatio(el, mid);
        if (fits(el, box)) low = mid;
        else high = mid;
    }
    return low;
}

function fitAll(targets: readonly FitTarget[]): void {
    const ratios = targets.map((target) => {
        setRatio(target.el, 1);
        const ratio = target.block ? blockRatio(target) : lineRatio(target);
        setRatio(target.el, ratio);
        return { group: target.group, ratio };
    });
    groupMin(ratios).forEach((ratio, i) => {
        const { el } = targets[i];
        setRatio(el, ratio);
        el.dataset.fitReady = '';
    });
}

function targetOf(el: HTMLElement): FitTarget | null {
    const box = el.parentElement?.closest<HTMLElement>(BOX_SELECTOR) ?? el.parentElement;
    if (!box) return null;
    return { el, box, block: el.dataset.fit === 'block', group: el.dataset.fitGroup || undefined };
}

function collect(root: ParentNode): FitTarget[] {
    const found = Array.from(root.querySelectorAll<HTMLElement>(SELECTOR));
    if (root instanceof HTMLElement && root.matches(SELECTOR)) found.unshift(root);
    return found.map(targetOf).filter((target): target is FitTarget => target !== null);
}

type FitRegistry = {
    targets: FitTarget[];
    active: boolean;
};

function addedTargets(records: MutationRecord[], known: Set<HTMLElement>): FitTarget[] {
    return records
        .flatMap((record) => Array.from(record.addedNodes))
        .filter((node): node is HTMLElement => node instanceof HTMLElement)
        .flatMap(collect)
        .filter((target) => !known.has(target.el) && known.add(target.el));
}

function touchedTargets(records: MutationRecord[], targets: readonly FitTarget[]): FitTarget[] {
    const touched = new Set(records.map((record) => (record.target as Element).closest?.(SELECTOR)));
    return targets.filter((target) => touched.has(target.el));
}

export function mountTextFit(root: ParentNode = document): () => void {
    const registry: FitRegistry = { targets: [], active: true };
    const refit = () => {
        if (registry.active) fitAll(registry.targets);
    };
    const resizer = new ResizeObserver(refit);
    const watch = (added: FitTarget[]) => {
        registry.targets = registry.targets.concat(added);
        for (const box of new Set(added.map((target) => target.box))) resizer.observe(box);
        fitAll(added);
    };
    const onMutations = (records: MutationRecord[]) => {
        if (!registry.active) return;
        const retouched = touchedTargets(records, registry.targets);
        if (retouched.length) fitAll(retouched);
        const added = addedTargets(records, new Set(registry.targets.map((target) => target.el)));
        if (added.length) watch(added);
    };

    watch(collect(root));
    const adder = new MutationObserver(onMutations);
    adder.observe(root, { childList: true, subtree: true });
    void document.fonts.ready.then(refit);
    document.fonts.addEventListener('loadingdone', refit);

    return () => {
        registry.active = false;
        resizer.disconnect();
        adder.disconnect();
        document.fonts.removeEventListener('loadingdone', refit);
    };
}
