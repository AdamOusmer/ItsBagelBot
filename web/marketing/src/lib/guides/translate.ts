// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { GuideContent } from './types';

export type GuideStrings = Record<string, string>;

const LITERAL = new Set(['slug', 'id', 'kind', 'tone', 'screen', 'path', 'who', 'storageKey', 'sample']);

interface Found {
    ids: string[];
    blocks: string[];
}

export function guideIds(guide: GuideContent): Found {
    const found: Found = { ids: [], blocks: [] };
    walk(guide.meta, 'meta', found);
    for (const section of guide.sections) walk(section, section.id, found);
    return found;
}

function walk(node: unknown, prefix: string, found: Found): void {
    if (Array.isArray(node)) {
        node.forEach((child, i) => visit(child, `${prefix}.${i}`, found));
        return;
    }
    if (!isObject(node)) return;
    for (const [key, value] of Object.entries(node)) {
        if (isStructure(node, key)) continue;
        if (key === 'blocks') walkBlocks(value as unknown[], prefix, found);
        else visit(value, `${prefix}.${key}`, found);
    }
}

function isObject(node: unknown): node is Record<string, unknown> {
    return !!node && typeof node === 'object';
}

function isStructure(node: Record<string, unknown>, key: string): boolean {
    if (LITERAL.has(key)) return true;
    return key === 'name' && node.kind === 'widget';
}

function walkBlocks(blocks: unknown[], prefix: string, found: Found): void {
    blocks.forEach((block, i) => {
        const id = `${prefix}.b${i}`;
        found.blocks.push(id);
        walk(block, id, found);
    });
}

function visit(value: unknown, id: string, found: Found): void {
    if (typeof value === 'string') found.ids.push(id);
    else walk(value, id, found);
}

export function translate(guide: GuideContent, strings: GuideStrings): GuideContent {
    const copy = structuredClone(guide);
    for (const [id, text] of Object.entries(strings)) set(copy, id, text);
    return copy;
}

function set(guide: GuideContent, id: string, text: string): void {
    const [scope, ...trail] = id.split('.');
    const last = trail.pop();
    if (last === undefined) return;
    let node = scopeOf(guide, scope);
    for (const segment of trail) node = step(node, segment);
    if (node) node[last] = text;
}

type Node = Record<string, unknown> | undefined;

function scopeOf(guide: GuideContent, scope: string): Node {
    if (scope === 'meta') return guide.meta as unknown as Record<string, unknown>;
    return guide.sections.find((s) => s.id === scope) as unknown as Node;
}

function step(node: Node, segment: string): Node {
    if (!node) return undefined;
    const block = /^b(\d+)$/.exec(segment);
    if (block) return (node.blocks as Node[] | undefined)?.[Number(block[1])];
    node[segment] ??= {};
    return node[segment] as Node;
}
