// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

(() => {
    const SLACK = 1;
    const ELLIPSIS_DISPLAYS = new Set(['block', 'inline-block', 'list-item', 'table-cell']);
    const STRESS_SKIP = 'script, style, svg, canvas';
    const CLIP_SKIP = 'svg, canvas, .bb-sr-only, .bb-mobile-menu, [popover]';

    const box = (r) => ({ left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width });
    const centerY = (r) => (r.top + r.bottom) / 2;
    const isInside = (r, outer) =>
        r.left >= outer.left - SLACK && r.right <= outer.right + SLACK &&
        r.top >= outer.top - SLACK && r.bottom <= outer.bottom + SLACK;
    const intersects = (a, b) => a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top;
    const hasSize = (r) => r.width > 0 && r.height > 0;

    function pairs(list) {
        return list.flatMap((a, i) => list.slice(i + 1).map((b) => [a, b]));
    }

    function isVisible(el) {
        if (!el || !hasSize(el.getBoundingClientRect())) return false;
        const style = getComputedStyle(el);
        return style.visibility !== 'hidden' && style.display !== 'none';
    }

    function navParts() {
        return {
            inner: document.querySelector('.bb-nav__inner'),
            hamburger: document.querySelector('.bb-hamburger'),
            brand: document.querySelector('.bb-nav__brand'),
            links: Array.from(document.querySelectorAll('.bb-nav__links .bb-nav-link')),
            lang: document.querySelector('.bb-nav__actions .bb-lang-switch'),
            langTrigger: document.querySelector('.bb-nav__actions .bb-lang-switch__trigger'),
            cta: document.querySelector('.bb-nav__cta'),
        };
    }

    function isCollapsed(parts) {
        return [isVisible(parts.hamburger), parts.links.every((l) => !isVisible(l)), !isVisible(parts.lang)].every(Boolean);
    }

    function isExpanded(parts) {
        return [
            !isVisible(parts.hamburger),
            parts.links.length > 0,
            parts.links.every(isVisible),
            !parts.lang || isVisible(parts.lang),
            !parts.cta || isVisible(parts.cta),
        ].every(Boolean);
    }

    function navBoxes(parts) {
        const named = [
            ['brand', 'brand', parts.brand],
            ...parts.links.map((l, i) => [`link${i}`, `link[${i}] ${(l.textContent || '').trim()}`, l]),
            ['langSwitch', 'langSwitch', parts.langTrigger],
            ['cta', 'cta', parts.cta],
        ];
        return named
            .filter(([, , el]) => el)
            .map(([id, label, el]) => ({ id, label, rect: el.getBoundingClientRect() }));
    }

    function overlapsOf(boxes) {
        return pairs(boxes)
            .filter(([a, b]) => intersects(a.rect, b.rect))
            .map(([a, b]) => `${a.label} x ${b.label}`);
    }

    function misalignedOf(items, pillCenter) {
        const offPill = items
            .filter((item) => Math.abs(item.centerY - pillCenter) > SLACK)
            .map((item) => `${item.label} vs pill center (${(item.centerY - pillCenter).toFixed(1)}px)`);
        const offEachOther = pairs(items)
            .filter(([a, b]) => Math.abs(a.centerY - b.centerY) > SLACK)
            .map(([a, b]) => `${a.label} vs ${b.label} (${(a.centerY - b.centerY).toFixed(1)}px)`);
        return [...offPill, ...offEachOther];
    }

    function expandedNav(parts, innerRect, base) {
        const boxes = navBoxes(parts);
        const outside = boxes.filter((b) => !isInside(b.rect, innerRect)).map((b) => b.label);
        const overlaps = overlapsOf(boxes);
        const items = boxes.map((b) => ({ id: b.id, label: b.label, centerY: centerY(b.rect) }));
        const misaligned = misalignedOf(items, base.innerCenterY);
        const ok = outside.length + overlaps.length + misaligned.length === 0;
        return { state: ok ? 'expanded' : 'broken-expanded', outside, overlaps, misaligned, items, ...base };
    }

    function checkNav() {
        const parts = navParts();
        if (!parts.inner) return { state: 'error', reason: 'missing .bb-nav__inner' };
        const innerRect = parts.inner.getBoundingClientRect();
        const base = { innerHeight: innerRect.height, innerCenterY: centerY(innerRect) };
        if (isCollapsed(parts)) return { state: 'collapsed', ...base };
        if (isExpanded(parts)) return expandedNav(parts, innerRect, base);
        return {
            state: 'half',
            hamburger: isVisible(parts.hamburger),
            visibleLinks: parts.links.filter(isVisible).length,
            lang: isVisible(parts.lang),
            ...base,
        };
    }

    function menuState() {
        const toggle = document.querySelector('[data-menu-toggle]');
        const menu = document.querySelector('[data-mobile-menu]');
        return {
            expanded: toggle?.getAttribute('aria-expanded') === 'true',
            open: Boolean(menu?.classList.contains('is-open')),
            hasLanguageSwitch: Boolean(menu?.querySelector('.bb-lang-switch')),
        };
    }

    function glyphsOutside(container) {
        const rect = container.getBoundingClientRect();
        return Array.from(document.querySelectorAll('[data-hero-glyph]'))
            .map((g) => ({ text: g.textContent, rect: g.getBoundingClientRect() }))
            .filter((g) => hasSize(g.rect) && !isInside(g.rect, rect))
            .map((g) => ({ text: g.text, rect: { left: g.rect.left, right: g.rect.right } }));
    }

    function asideFits(aside) {
        const a = aside.getBoundingClientRect();
        const viewport = { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
        return { asideRect: box(a), insideViewport: isInside(a, viewport), wideEnough: a.width >= 280 };
    }

    function checkHeroDesktop(minWidth) {
        const aside = document.querySelector('.aside');
        const h1 = document.querySelector('[data-hero-title]');
        const header = document.querySelector('header');
        if (![aside, h1, header].every(Boolean)) return { ok: false, reason: 'missing hero elements' };
        if (window.innerWidth < minWidth) return { ok: true, skipped: true };
        const fit = asideFits(aside);
        const outsideH1 = glyphsOutside(h1);
        const outsideHeader = glyphsOutside(header);
        const ok = [fit.insideViewport, fit.wideEnough, outsideH1.length === 0, outsideHeader.length === 0].every(Boolean);
        return { ok, ...fit, outsideH1, outsideHeader };
    }

    function lineClips(line) {
        const lineRect = line.getBoundingClientRect();
        return Array.from(line.querySelectorAll('[data-hero-glyph]'))
            .map((g) => ({ text: g.textContent, rect: g.getBoundingClientRect() }))
            .filter((g) => hasSize(g.rect))
            .filter((g) => g.rect.left < lineRect.left - SLACK || g.rect.right > lineRect.right + SLACK)
            .map((g) => ({ text: g.text, glyphRect: { left: g.rect.left, right: g.rect.right } }));
    }

    function checkHeroGlyphClip() {
        return Array.from(document.querySelectorAll('[data-hero-line]')).flatMap(lineClips);
    }

    function checkHeroGeometry() {
        const split = document.querySelector('.split');
        const h1 = document.querySelector('[data-hero-title]');
        if (!split || !h1) return null;
        const splitStyle = getComputedStyle(split);
        if (splitStyle.display !== 'grid') return null;
        const cols = splitStyle.gridTemplateColumns.split(' ').map(parseFloat).filter((v) => !Number.isNaN(v));
        return { cols, fontSize: parseFloat(getComputedStyle(h1).fontSize) };
    }

    function hasDirectText(el) {
        return Array.from(el.childNodes).some((child) => child.nodeType === 3 && child.textContent.trim() !== '');
    }

    function showsEllipsis(style) {
        return style.textOverflow === 'ellipsis' && ELLIPSIS_DISPLAYS.has(style.display);
    }

    function clipsX(style) {
        return ['hidden', 'clip'].includes(style.overflowX);
    }

    function isClippedText(el) {
        if (el.closest(CLIP_SKIP) || !hasDirectText(el)) return false;
        const style = getComputedStyle(el);
        const clipping = [isVisible(el), clipsX(style), !showsEllipsis(style)].every(Boolean);
        return clipping && el.scrollWidth > el.clientWidth + SLACK;
    }

    function checkClippedText() {
        return Array.from(document.body.querySelectorAll('*'))
            .filter(isClippedText)
            .map((el) => ({
                tag: el.tagName.toLowerCase(),
                className: typeof el.className === 'string' ? el.className : '',
                text: (el.textContent || '').trim().slice(0, 60),
                scrollWidth: el.scrollWidth,
                clientWidth: el.clientWidth,
            }));
    }

    function stressTarget(node) {
        return node.textContent.trim() !== '' && !node.parentElement?.closest(STRESS_SKIP);
    }

    function pseudoLocaleStress() {
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
        const nodes = [];
        for (let node = walker.nextNode(); node; node = walker.nextNode()) {
            if (stressTarget(node)) nodes.push(node);
        }
        for (const n of nodes) {
            const t = n.textContent;
            n.textContent = t + t.slice(0, Math.ceil(t.length / 2));
        }
    }

    function hasHorizontalOverflow() {
        const doc = document.documentElement;
        return doc.scrollWidth > doc.clientWidth + SLACK;
    }

    function navIdle() {
        const nav = document.querySelector('.bb-nav');
        return !nav || nav.getAnimations({ subtree: true }).every((a) => a.playState !== 'running');
    }

    function glyphsSettled() {
        const glyphs = Array.from(document.querySelectorAll('[data-hero-glyph]'));
        const settled = (g) =>
            parseFloat(getComputedStyle(g).opacity) >= 0.99 && g.getAnimations().every((a) => a.playState === 'finished');
        return glyphs.length > 0 && glyphs.every(settled);
    }

    function twoFrames() {
        return new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve(undefined))));
    }

    window.layoutProbes = {
        checkNav,
        menuState,
        checkHeroDesktop,
        checkHeroGlyphClip,
        checkHeroGeometry,
        checkClippedText,
        pseudoLocaleStress,
        hasHorizontalOverflow,
        navIdle,
        glyphsSettled,
        twoFrames,
    };
})();
