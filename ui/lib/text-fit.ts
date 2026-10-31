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

const TAG = 'bb-fit';
const BOX_SELECTOR = '.bb-fit-box';
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
        targets[i].el.classList.add('is-fit');
    });
}

export function fitSlotOf(el: HTMLElement): { block: boolean; group: string | undefined } {
    return { block: el.getAttribute('mode') === 'block', group: el.getAttribute('group') || undefined };
}

function targetOf(el: HTMLElement): FitTarget | null {
    const box = el.parentElement?.closest<HTMLElement>(BOX_SELECTOR) ?? el.parentElement;
    if (!box) return null;
    return { el, box, ...fitSlotOf(el), tracking: 0 };
}

type FitRegistry = {
    targets: Map<HTMLElement, FitTarget>;
    boxes: Map<HTMLElement, Set<FitTarget>>;
    pending: Set<FitTarget>;
    resizer: ResizeObserver;
    texts: MutationObserver;
};

function touchedTargets(records: MutationRecord[], registry: FitRegistry, selector: string): FitTarget[] {
    const touched = new Set<FitTarget>();
    for (const record of records) {
        const node = record.target;
        const owner = node instanceof Element ? node : node.parentElement;
        const el = owner?.closest<HTMLElement>(selector);
        const target = el ? registry.targets.get(el) : undefined;
        if (target) touched.add(target);
    }
    return Array.from(touched);
}

const sections = new Map<Element, Set<FitTarget>>();
const parked = new Map<FitTarget, Element>();

// A content-visibility:auto subtree that is skipped would be forced through style and layout by any measurement.
function skippedSection(el: HTMLElement): Element | null {
    if (typeof el.checkVisibility !== 'function' || el.checkVisibility({ contentVisibilityAuto: true })) return null;
    return el.closest('.bb-section--page') ?? el.closest('section');
}

function unpark(target: FitTarget): void {
    const section = parked.get(target);
    if (!section) return;
    parked.delete(target);
    const members = sections.get(section);
    members?.delete(target);
    if (!members?.size) sections.delete(section);
}

function park(target: FitTarget, section: Element): void {
    if (parked.get(target) === section) return;
    unpark(target);
    parked.set(target, section);
    const members = sections.get(section);
    if (members) members.add(target);
    else sections.set(section, new Set([target]));
}

function measurable(targets: FitTarget[]): FitTarget[] {
    return targets.filter((target) => {
        const section = skippedSection(target.el);
        if (section) park(target, section);
        else unpark(target);
        return !section;
    });
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

function bindBox(registry: FitRegistry, target: FitTarget): void {
    const siblings = registry.boxes.get(target.box);
    if (siblings) siblings.add(target);
    else {
        registry.boxes.set(target.box, new Set([target]));
        registry.resizer.observe(target.box);
    }
}

function unbindBox(registry: FitRegistry, target: FitTarget): void {
    const siblings = registry.boxes.get(target.box);
    siblings?.delete(target);
    if (siblings?.size) return;
    registry.boxes.delete(target.box);
    registry.resizer.unobserve(target.box);
}

function register(registry: FitRegistry, added: FitTarget[]): void {
    for (const target of added) {
        registry.targets.set(target.el, target);
        bindBox(registry, target);
    }
}

function prune(registry: FitRegistry): void {
    for (const [el, target] of registry.targets) {
        if (el.isConnected) continue;
        registry.targets.delete(el);
        unpark(target);
        unbindBox(registry, target);
    }
}

function refit(registry: FitRegistry, subset: FitTarget[]): void {
    if (subset.length === 0) return;
    const ready = measurable(withGroupPeers(registry, Array.from(new Set(subset))));
    if (ready.length) fitAll(ready);
}

function boxTargets(registry: FitRegistry, entries: ResizeObserverEntry[]): FitTarget[] {
    return entries.flatMap((entry) => Array.from(registry.boxes.get(entry.target as HTMLElement) ?? []));
}

let shared: FitRegistry | null = null;

function sharedRegistry(): FitRegistry {
    if (shared) return shared;
    const registry: FitRegistry = {
        targets: new Map(),
        boxes: new Map(),
        pending: new Set(),
        resizer: new ResizeObserver((entries) => {
            prune(registry);
            refit(registry, boxTargets(registry, entries));
        }),
        texts: new MutationObserver((records) => {
            prune(registry);
            refit(registry, touchedTargets(records, registry, TAG));
        }),
    };
    const refitAll = () => {
        prune(registry);
        fitAll(measurable(Array.from(registry.targets.values())));
    };
    void document.fonts.ready.then(refitAll);
    document.fonts.addEventListener('loadingdone', refitAll);
    shared = registry;
    return registry;
}

// A parser connects the element before its text exists, so connects are fitted together once the tick ends.
function queueFit(registry: FitRegistry, target: FitTarget): void {
    if (registry.pending.size === 0) queueMicrotask(() => refit(registry, takePending(registry)));
    registry.pending.add(target);
}

function takePending(registry: FitRegistry): FitTarget[] {
    const batch = Array.from(registry.pending).filter((queued) => registry.targets.get(queued.el) === queued);
    registry.pending.clear();
    return batch;
}

function connectFit(el: HTMLElement): void {
    const target = targetOf(el);
    if (!target) return;
    const registry = sharedRegistry();
    register(registry, [target]);
    registry.texts.observe(el, { childList: true, characterData: true, subtree: true });
    queueFit(registry, target);
}

function disconnectFit(el: HTMLElement): void {
    const target = shared?.targets.get(el);
    if (!shared || !target) return;
    shared.targets.delete(el);
    shared.pending.delete(target);
    unpark(target);
    unbindBox(shared, target);
}

function onSectionState(event: Event): void {
    const root = event.target as Element | null;
    if (!shared || (event as Event & { skipped?: boolean }).skipped !== false || typeof root?.contains !== 'function') return;
    const arrived: FitTarget[] = [];
    for (const [section, members] of sections) {
        if (root.contains(section)) arrived.push(...members);
    }
    refit(shared, arrived);
}

function reslotFit(el: HTMLElement): void {
    const target = shared?.targets.get(el);
    if (!shared || !target) return;
    Object.assign(target, fitSlotOf(el));
    queueFit(shared, target);
}

export function defineTextFit(): void {
    if (typeof customElements === 'undefined' || customElements.get(TAG)) return;
    // Declared here because HTMLElement does not exist where the module is imported for SSR or tests.
    class BbFit extends HTMLElement {
        static observedAttributes = ['mode', 'group'];

        connectedCallback(): void {
            connectFit(this);
        }

        disconnectedCallback(): void {
            disconnectFit(this);
        }

        attributeChangedCallback(): void {
            reslotFit(this);
        }
    }
    customElements.define(TAG, BbFit);
    document.addEventListener('contentvisibilityautostatechange', onSectionState, true);
}
