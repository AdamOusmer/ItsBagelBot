// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { afterEach, expect, test } from 'bun:test';
import { copyFlash, copyText } from '../lib/clipboard';

const originals = new Map<string, PropertyDescriptor | undefined>();
function globalValue(name: string, value: unknown) {
  if (!originals.has(name)) originals.set(name, Object.getOwnPropertyDescriptor(globalThis, name));
  Object.defineProperty(globalThis, name, { value, configurable: true });
}
afterEach(() => {
  for (const [name, descriptor] of originals) {
    if (descriptor) Object.defineProperty(globalThis, name, descriptor);
    else Reflect.deleteProperty(globalThis, name);
  }
  originals.clear();
});

test('successful clipboard writes return true and preserve exact text', async () => {
  let written = '';
  globalValue('navigator', { clipboard: { async writeText(text: string) { written = text; } } });
  expect(await copyText('hello\nworld')).toBe(true);
  expect(written).toBe('hello\nworld');
});

test('rejected writes do not claim a copied confirmation', async () => {
  globalValue('navigator', { clipboard: { async writeText() { throw new Error('denied'); } } });
  const states: boolean[] = [];
  expect(await copyText('hello')).toBe(false);
  await copyFlash('hello', (on) => states.push(on));
  expect(states).toEqual([false]);
});

test('missing browser APIs fail quietly during server rendering', async () => {
  globalValue('navigator', undefined);
  globalValue('document', undefined);
  expect(await copyText('hello', { legacyFallback: true })).toBe(false);
});

test('legacy fallback reports actual copy result and restores focus/selection on failure', async () => {
  globalValue('navigator', {});
  const actions: string[] = [];
  class Element { focus() { actions.push('focus'); } }
  globalValue('HTMLElement', Element);
  const range = { cloneRange: () => range };
  const field = { value: '', style: { cssText: '' }, select: () => actions.push('select'), remove: () => actions.push('remove') };
  globalValue('document', {
    activeElement: new Element(),
    getSelection: () => ({ rangeCount: 1, getRangeAt: () => range, removeAllRanges: () => actions.push('clearSelection'), addRange: () => actions.push('restoreSelection') }),
    createElement: () => field,
    body: { appendChild: () => actions.push('append') },
    execCommand: () => { throw new Error('unsupported'); }
  });
  expect(await copyText('legacy', { legacyFallback: true })).toBe(false);
  expect(field.value).toBe('legacy');
  expect(actions).toEqual(['append', 'select', 'remove', 'focus', 'clearSelection', 'restoreSelection']);
});
