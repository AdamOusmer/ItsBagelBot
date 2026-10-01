// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';

const LOCALES = JSON.parse(readFileSync(new URL('../../../locales/manifest.json', import.meta.url), 'utf8'));
const PAGES = ['/', '/pricing', '/contact', '/import', '/command-builder'];
const VIEWPORTS = [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
];
const SKIP = [
    'script', 'style', 'svg *', 'canvas', '[popover]', '.bb-mobile-menu *', '.play__feed *', '.bb-skip-link',
    '[data-chat] > .line', '[data-chat] > .line *',
    '[data-fit]', '[data-fit] *',
].join(', ');
const TOLERANCE_PX = 1;
const MIN_FIT_RATIO = 0.6;
const REPORT_LIMIT = 40;

if (process.env.LOCALE_LAYOUT_BASE) test.use({ baseURL: process.env.LOCALE_LAYOUT_BASE });

function localeUrl(locale, path) {
    const prefix = locale === 'en' ? '' : `/${locale}`;
    return `${prefix}${path === '/' ? '/' : `${path}/`}`;
}

// These run inside the page: page.evaluate serializes one function, so they are installed as an init script.
function pathOf(el) {
    const parts = [];
    for (let node = el; node && node !== document.body; node = node.parentElement) {
        const index = Array.prototype.indexOf.call(node.parentElement.children, node) + 1;
        const kept = Array.from(node.classList).filter((c) => !/^(is-|astro-|data-)/.test(c)).slice(0, 2);
        parts.unshift(`${node.tagName.toLowerCase()}${kept.length ? `.${kept.join('.')}` : ''}:${index}`);
    }
    return parts.join('>');
}

function hugsText(el) {
    return el.children.length > 0 && Array.from(el.children).every((child) => child.hasAttribute('data-fit') || hugsText(child));
}

function hasSize(rect) {
    return rect.width > 0 || rect.height > 0;
}

function boxesOf(skip) {
    const boxes = {};
    for (const el of document.body.querySelectorAll('*')) {
        if (el.matches(skip) || hugsText(el)) continue;
        const r = el.getBoundingClientRect();
        if (hasSize(r)) boxes[pathOf(el)] = [Math.round(r.x), Math.round(r.y), Math.round(r.width), Math.round(r.height)];
    }
    return boxes;
}

function fitOverflow(el) {
    const box = el.parentElement.closest('[data-fit-box]') ?? el.parentElement;
    const x = el.scrollWidth - box.clientWidth;
    if (x > 1) return `overflows x by ${x}px (ratio ${el.style.getPropertyValue('--bb-fit') || '1'}, text ${el.scrollWidth}, box ${box.clientWidth})`;
    if (el.dataset.fit !== 'block') return null;
    const style = getComputedStyle(box);
    const contentBottom = box.getBoundingClientRect().bottom - parseFloat(style.paddingBottom) - parseFloat(style.borderBottomWidth);
    const y = Math.round(el.getBoundingClientRect().bottom - contentBottom);
    return y > 1 ? `overflows y by ${y}px` : null;
}

function fitProblemOf(el, minRatio) {
    if (!el.hasAttribute('data-fit-ready')) return 'not fitted';
    const ratio = parseFloat(getComputedStyle(el).getPropertyValue('--bb-fit'));
    if (ratio < minRatio) return `ratio ${ratio.toFixed(2)}`;
    return hasSize(el.getBoundingClientRect()) ? fitOverflow(el) : null;
}

function fitProblemsOf(minRatio) {
    const problems = [];
    for (const el of document.querySelectorAll('[data-fit]')) {
        const problem = fitProblemOf(el, minRatio);
        if (problem) problems.push(`${problem}: ${pathOf(el)} "${(el.textContent ?? '').trim().slice(0, 40)}"`);
    }
    return problems;
}

const PAGE_HELPERS = [pathOf, hugsText, hasSize, boxesOf, fitOverflow, fitProblemOf, fitProblemsOf];
const PAGE_HELPERS_SOURCE = `${PAGE_HELPERS.map(String).join('\n')}\nwindow.__localeLayout = { boxesOf, fitProblemsOf };`;

function measure({ skip, minRatio }) {
    const { boxesOf, fitProblemsOf } = window.__localeLayout;
    return { boxes: boxesOf(skip), fitProblems: fitProblemsOf(minRatio) };
}

function topLevelIndex(path) {
    const top = path.split('>')[0];
    return Number(top.slice(top.lastIndexOf(':') + 1));
}

// Prose pages grow with their content, so boxes after <main> may sit lower without moving.
function mainIndexOf(page, english) {
    if (page === '/') return null;
    const main = Object.keys(english).find((path) => /^main(\.[^:>]*)?:\d+$/.test(path));
    return main ? topLevelIndex(main) : null;
}

function differingDims(path, { english, other, mainIndex }) {
    const a = english[path];
    const b = other[path];
    if (!b) return ['missing'];
    const ignoresY = mainIndex !== null && topLevelIndex(path) > mainIndex;
    return ['x', 'y', 'w', 'h'].filter((dim, i) => !(dim === 'y' && ignoresY) && Math.abs(a[i] - b[i]) > TOLERANCE_PX);
}

function nearestKnownAncestor(path, known) {
    let ancestor = path;
    while (ancestor.includes('>')) {
        ancestor = ancestor.slice(0, ancestor.lastIndexOf('>'));
        if (known[ancestor]) return ancestor;
    }
    return null;
}

function describeDifference(path, dims, english, other) {
    const was = english[path].join(',');
    const now = other[path]?.join(',') ?? 'missing';
    return `[${dims.join('')}] ${path.split('>').slice(-4).join('>')} en=${was} vs ${now}`;
}

function topMostDifferences(page, english, other) {
    const context = { english, other, mainIndex: mainIndexOf(page, english) };
    const dims = new Map(Object.keys(english).map((path) => [path, differingDims(path, context)]));
    const report = [];
    for (const [path, changed] of dims) {
        if (changed.length === 0) continue;
        const ancestor = nearestKnownAncestor(path, english);
        if (ancestor && dims.get(ancestor).length > 0) continue;
        report.push(describeDifference(path, changed, english, other));
    }
    return report;
}

async function snapshotLocale(page, locale, path) {
    await page.goto(localeUrl(locale, path), { waitUntil: 'networkidle' });
    await page.evaluate(() => document.fonts.ready);
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.waitForTimeout(250);
    return page.evaluate(measure, { skip: SKIP, minRatio: MIN_FIT_RATIO });
}

async function checkPage(page, path) {
    const failures = [];
    const snapshots = {};
    for (const locale of LOCALES) {
        const { boxes, fitProblems } = await snapshotLocale(page, locale, path);
        snapshots[locale] = boxes;
        failures.push(...fitProblems.map((problem) => `${locale} ${path} fit ${problem}`));
    }
    for (const locale of LOCALES.filter((code) => code !== 'en')) {
        const lines = topMostDifferences(path, snapshots.en, snapshots[locale]);
        failures.push(...lines.map((line) => `${locale} ${path} ${line}`));
    }
    return failures;
}

function summary(failures) {
    const shown = failures.slice(0, REPORT_LIMIT);
    if (failures.length > REPORT_LIMIT) shown.push(`... and ${failures.length - REPORT_LIMIT} more`);
    return shown.join('\n');
}

test.describe('locale-stable layout', () => {
    for (const viewport of VIEWPORTS) {
        test(`every locale shares the English geometry at ${viewport.width}x${viewport.height}`, async ({ browser }) => {
            test.setTimeout(300_000);
            const context = await browser.newContext({ viewport, reducedMotion: 'reduce', deviceScaleFactor: 1 });
            const page = await context.newPage();
            await page.addInitScript({ content: PAGE_HELPERS_SOURCE });
            const failures = [];
            for (const path of PAGES) failures.push(...(await checkPage(page, path)));
            await context.close();
            expect(failures, summary(failures)).toEqual([]);
        });
    }
});
