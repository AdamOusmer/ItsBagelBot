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

function measure({ skip, minRatio }) {
    const pathOf = (el) => {
        const parts = [];
        for (let node = el; node && node !== document.body; node = node.parentElement) {
            const index = Array.prototype.indexOf.call(node.parentElement.children, node) + 1;
            const kept = Array.from(node.classList).filter((c) => !/^(is-|astro-|data-)/.test(c)).slice(0, 2);
            parts.unshift(`${node.tagName.toLowerCase()}${kept.length ? `.${kept.join('.')}` : ''}:${index}`);
        }
        return parts.join('>');
    };

    const hugs = (el) =>
        el.children.length > 0 && Array.from(el.children).every((child) => child.hasAttribute('data-fit') || hugs(child));

    const boxes = {};
    for (const el of document.body.querySelectorAll('*')) {
        if (el.matches(skip) || hugs(el)) continue;
        const r = el.getBoundingClientRect();
        if (r.width === 0 && r.height === 0) continue;
        boxes[pathOf(el)] = [Math.round(r.x), Math.round(r.y), Math.round(r.width), Math.round(r.height)];
    }

    const fitProblems = [];
    for (const el of document.querySelectorAll('[data-fit]')) {
        const where = `${pathOf(el)} "${(el.textContent ?? '').trim().slice(0, 40)}"`;
        if (!el.hasAttribute('data-fit-ready')) {
            fitProblems.push(`not fitted: ${where}`);
            continue;
        }
        const ratio = parseFloat(getComputedStyle(el).getPropertyValue('--bb-fit'));
        if (!Number.isNaN(ratio) && ratio < minRatio) fitProblems.push(`ratio ${ratio.toFixed(2)}: ${where}`);
        const r = el.getBoundingClientRect();
        if (r.width === 0 && r.height === 0) continue;
        const box = el.parentElement.closest('[data-fit-box]') ?? el.parentElement;
        if (el.scrollWidth > box.clientWidth + 1) fitProblems.push(`overflows x by ${el.scrollWidth - box.clientWidth}px: ${where}`);
        if (el.dataset.fit !== 'block') continue;
        const style = getComputedStyle(box);
        const contentBottom = box.getBoundingClientRect().bottom - parseFloat(style.paddingBottom) - parseFloat(style.borderBottomWidth);
        if (r.bottom > contentBottom + 1) fitProblems.push(`overflows y by ${Math.round(r.bottom - contentBottom)}px: ${where}`);
    }
    return { boxes, fitProblems };
}

function ignoresY(page, path, mainIndex) {
    if (page === '/' || mainIndex === null) return false;
    const top = path.split('>')[0];
    return Number(top.slice(top.lastIndexOf(':') + 1)) > mainIndex;
}

function differingDims(page, path, english, other, mainIndex) {
    const a = english[path];
    const b = other[path];
    if (!b) return ['missing'];
    return ['x', 'y', 'w', 'h'].filter((dim, i) => {
        if (dim === 'y' && ignoresY(page, path, mainIndex)) return false;
        return Math.abs(a[i] - b[i]) > TOLERANCE_PX;
    });
}

function topMostDifferences(page, english, other) {
    const main = Object.keys(english).find((path) => /^main(\.[^:>]*)?:\d+$/.test(path));
    const mainIndex = main ? Number(main.slice(main.lastIndexOf(':') + 1)) : null;
    const differs = new Map();
    const dimsOf = (path) => {
        if (!differs.has(path)) differs.set(path, differingDims(page, path, english, other, mainIndex));
        return differs.get(path);
    };
    const report = [];
    for (const path of Object.keys(english)) {
        const dims = dimsOf(path);
        if (dims.length === 0) continue;
        let ancestor = path;
        let ancestorDiffers = false;
        while (ancestor.includes('>')) {
            ancestor = ancestor.slice(0, ancestor.lastIndexOf('>'));
            if (!english[ancestor]) continue;
            ancestorDiffers = dimsOf(ancestor).length > 0;
            break;
        }
        if (ancestorDiffers) continue;
        const was = english[path].join(',');
        const now = other[path]?.join(',') ?? 'missing';
        report.push(`[${dims.join('')}] ${path.split('>').slice(-4).join('>')} en=${was} vs ${now}`);
    }
    return report;
}

test.describe('locale-stable layout', () => {
    for (const viewport of VIEWPORTS) {
        test(`every locale shares the English geometry at ${viewport.width}x${viewport.height}`, async ({ browser }) => {
            test.setTimeout(300_000);
            const context = await browser.newContext({ viewport, reducedMotion: 'reduce', deviceScaleFactor: 1 });
            const page = await context.newPage();
            const failures = [];

            for (const path of PAGES) {
                const snapshots = {};
                for (const locale of LOCALES) {
                    await page.goto(localeUrl(locale, path), { waitUntil: 'networkidle' });
                    await page.evaluate(() => document.fonts.ready);
                    await page.evaluate(() => window.scrollTo(0, 0));
                    await page.waitForTimeout(250);
                    const { boxes, fitProblems } = await page.evaluate(measure, { skip: SKIP, minRatio: MIN_FIT_RATIO });
                    snapshots[locale] = boxes;
                    for (const problem of fitProblems) failures.push(`${locale} ${path} fit ${problem}`);
                }
                for (const locale of LOCALES) {
                    if (locale === 'en') continue;
                    for (const line of topMostDifferences(path, snapshots.en, snapshots[locale])) {
                        failures.push(`${locale} ${path} ${line}`);
                    }
                }
            }

            await context.close();
            const shown = failures.slice(0, REPORT_LIMIT);
            if (failures.length > REPORT_LIMIT) shown.push(`... and ${failures.length - REPORT_LIMIT} more`);
            expect(failures, shown.join('\n')).toEqual([]);
        });
    }
});
