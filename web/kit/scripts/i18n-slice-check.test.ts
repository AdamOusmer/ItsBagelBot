// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { assertCoverage, type UnitSlice } from './i18n-slice-check';
import { ENTRY } from './i18n-slice-units';

const node = (index: number) => `/w/.svelte-kit/generated/client-optimized/nodes/${index}.js`;
const EDITOR = '/w/src/lib/Editor.svelte';
const english = { nav: { home: 'Home' }, page: { title: 'Commands' }, editor: { save: 'Save' } };

function slice(demand: string[], keys: Record<string, string>, contexts: string[] = []): UnitSlice {
  const tree: Record<string, Record<string, string>> = {};
  for (const [key, value] of Object.entries(keys)) {
    const [branch, leaf] = key.split('.');
    (tree[branch] ??= {})[leaf] = value;
  }
  return { demand: new Set(demand), bodies: { en: JSON.stringify(tree), fr: JSON.stringify(tree) }, contexts };
}

function check(editorContexts: string[], editorKeys: Record<string, string>) {
  const slices = new Map<string, UnitSlice>([
    [node(0), slice(['nav.home'], { 'nav.home': 'Home' })],
    [node(1), slice([], {})],
    [node(6), slice(['nav.home', 'page.title'], { 'page.title': 'Commands' })],
    [EDITOR, slice(['editor.save', 'nav.home'], editorKeys, editorContexts)]
  ]);
  const routes = [{ id: '/(app)/commands', nodes: [6], errors: [] }];
  return () => assertCoverage({ slices, locales: ['en', 'fr'], ancestors: new Map([[6, new Set([0])]]) }, routes, english);
}

describe('assertCoverage', () => {
  test('a lazy module may rely on the slices of the page that imports it', () => {
    expect(check([node(6)], { 'editor.save': 'Save' })).not.toThrow();
  });

  test('a lazy module imported from the entry chunks may rely on nothing else', () => {
    expect(check([ENTRY], { 'editor.save': 'Save' })).toThrow('lacks 1 key(s), first nav.home');
    expect(check([ENTRY], { 'editor.save': 'Save', 'nav.home': 'Home' })).not.toThrow();
  });

  test('a lazy module reached from several pages may rely only on what all of them load', () => {
    expect(check([node(6), ENTRY], { 'editor.save': 'Save' })).toThrow('first nav.home');
  });
});
