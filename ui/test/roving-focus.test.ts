// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';

import { mountRovingFocus, nextIndex, rovingTarget, stepFor } from '../lib/roving-focus';

interface FakeItem {
  name: string;
  disabled?: boolean;
  focused?: boolean;
  matches(selector: string): boolean;
  contains(node: unknown): boolean;
  focus(): void;
}

function item(name: string, disabled = false): FakeItem {
  return {
    name,
    disabled,
    matches(selector) {
      return selector === ':disabled' ? this.disabled === true : false;
    },
    contains(node) {
      return node === this;
    },
    focus() {
      this.focused = true;
    },
  };
}

function root(items: FakeItem[]) {
  const listeners = new Map<string, (event: unknown) => void>();
  return {
    querySelectorAll: () => items,
    addEventListener: (type: string, fn: (event: unknown) => void) => listeners.set(type, fn),
    removeEventListener: (type: string) => listeners.delete(type),
    listeners,
  };
}

const textInput = { matches: (selector: string) => selector.startsWith('input') };

function key(k: string, target: unknown) {
  let prevented = false;
  return {
    key: k,
    target,
    preventDefault: () => {
      prevented = true;
    },
    get prevented() {
      return prevented;
    },
  };
}

describe('nextIndex', () => {
  test('arrows step along the orientation and clamp at the ends by default', () => {
    expect(nextIndex('ArrowDown', 0, 3)).toBe(1);
    expect(nextIndex('ArrowUp', 1, 3)).toBe(0);
    expect(nextIndex('ArrowUp', 0, 3)).toBe(0);
    expect(nextIndex('ArrowDown', 2, 3)).toBe(2);
    expect(nextIndex('ArrowRight', 0, 3)).toBeNull();
  });

  test('wrap cycles past either end', () => {
    expect(nextIndex('ArrowRight', 2, 3, { orientation: 'horizontal', wrap: true })).toBe(0);
    expect(nextIndex('ArrowLeft', 0, 3, { orientation: 'horizontal', wrap: true })).toBe(2);
  });

  test('Home and End jump to the edges in any orientation', () => {
    expect(nextIndex('Home', 2, 4, { orientation: 'horizontal' })).toBe(0);
    expect(nextIndex('End', 0, 4)).toBe(3);
  });

  test('from outside the list any move lands on the first item', () => {
    expect(nextIndex('ArrowDown', -1, 3)).toBe(0);
    expect(nextIndex('ArrowUp', -1, 3)).toBe(0);
  });

  test('an empty list and unrelated keys move nothing', () => {
    expect(nextIndex('ArrowDown', 0, 0)).toBeNull();
    expect(nextIndex('a', 0, 3)).toBeNull();
    expect(stepFor('ArrowLeft', 'both')).toBe(-1);
    expect(stepFor('ArrowDown', 'horizontal')).toBe(0);
  });
});

describe('rovingTarget', () => {
  test('skips disabled items', () => {
    const items = [item('a'), item('b', true), item('c')];
    const next = rovingTarget(root(items) as never, key('ArrowDown', items[0]) as never, { selector: 'x' });
    expect((next as unknown as FakeItem).name).toBe('c');
  });

  test('from a text field only ArrowDown enters the list, so the caret keeps Home, End and ArrowUp', () => {
    const items = [item('a'), item('b')];
    const list = root(items) as never;
    expect(rovingTarget(list, key('Home', textInput) as never, { selector: 'x' })).toBeNull();
    expect(rovingTarget(list, key('ArrowUp', textInput) as never, { selector: 'x' })).toBeNull();
    const next = rovingTarget(list, key('ArrowDown', textInput) as never, { selector: 'x' });
    expect((next as unknown as FakeItem).name).toBe('a');
  });
});

describe('mountRovingFocus', () => {
  test('focuses the next item, prevents the default scroll, and unbinds on dispose', () => {
    const items = [item('a'), item('b')];
    const list = root(items);
    const dispose = mountRovingFocus(list as never, { selector: 'x' });
    const event = key('ArrowDown', items[0]);
    list.listeners.get('keydown')?.(event);
    expect(items[1].focused).toBe(true);
    expect(event.prevented).toBe(true);
    dispose();
    expect(list.listeners.has('keydown')).toBe(false);
  });

  test('an unhandled key keeps its default', () => {
    const items = [item('a')];
    const list = root(items);
    mountRovingFocus(list as never, { selector: 'x' });
    const event = key('Tab', items[0]);
    list.listeners.get('keydown')?.(event);
    expect(event.prevented).toBe(false);
  });
});
