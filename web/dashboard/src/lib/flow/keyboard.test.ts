// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import type { Flow } from './model';
import { allowed, neighbor, neighborAfterRemove, readingOrder, resolveShortcut, stepDelta, stepIndex, type KeyLike, type Scopes, type Target } from './keyboard';

const key = (k: string, mods: Partial<KeyLike> = {}): KeyLike => ({ key: k, ctrlKey: false, metaKey: false, shiftKey: false, altKey: false, ...mods });

function el(tagName: string, within: string[] = [], id?: string): Target {
  return { tagName, id, closest: (selector: string) => (within.some((w) => selector.includes(w)) ? {} : null) };
}

const canvasEl = el('DIV');
const editorEl = el('DIV');
const nodeEl = el('DIV');
const nodeButtonEl = el('BUTTON', ['button']);
const scopes: Scopes = {
  canvas: (t) => t === canvasEl,
  editor: (t) => t === editorEl || t === canvasEl,
  node: (t) => t === nodeEl || t === nodeButtonEl
};

const flow: Flow = {
  id: 'f',
  name: 'f',
  enabled: true,
  nodes: [
    { id: 't', type: 'twitch.chat.command', x: 0, y: 0, config: {} },
    { id: 'c', type: 'cond.role', x: 368, y: 0, config: {} },
    { id: 'yes', type: 'reply.chat', x: 736, y: 0, config: {} },
    { id: 'no', type: 'reply.chat', x: 736, y: 200, config: {} },
    { id: 'lone', type: 'reply.chat', x: 1104, y: 0, config: {} }
  ],
  edges: [
    { id: 'e1', from: 't', port: 'next', to: 'c' },
    { id: 'e2', from: 'c', port: 'no', to: 'no' },
    { id: 'e3', from: 'c', port: 'yes', to: 'yes' }
  ]
};

describe('resolveShortcut', () => {
  const documented: [string, Partial<KeyLike>, Record<string, string>][] = [
    ['Delete', {}, { id: 'delete' }],
    ['Backspace', {}, { id: 'delete' }],
    ['Escape', {}, { id: 'escape' }],
    ['ArrowRight', {}, { id: 'walk', dir: 'right' }],
    ['ArrowUp', { shiftKey: true }, { id: 'nudge', dir: 'up' }],
    ['Enter', {}, { id: 'open' }],
    [' ', {}, { id: 'open' }],
    ['+', { shiftKey: true }, { id: 'zoom-in' }],
    ['=', {}, { id: 'zoom-in' }],
    ['-', {}, { id: 'zoom-out' }],
    ['0', {}, { id: 'fit' }],
    ['a', {}, { id: 'add' }],
    ['A', { shiftKey: true }, { id: 'add-other' }]
  ];

  test.each(documented)('maps %j with %j', (k, mods, expected) => {
    expect(resolveShortcut(key(k, mods))).toMatchObject(expected);
  });

  test('undo, redo, duplicate and save accept Cmd or Ctrl', () => {
    expect(resolveShortcut(key('z', { metaKey: true }))?.id).toBe('undo');
    expect(resolveShortcut(key('z', { ctrlKey: true }))?.id).toBe('undo');
    expect(resolveShortcut(key('Z', { metaKey: true, shiftKey: true }))?.id).toBe('redo');
    expect(resolveShortcut(key('y', { ctrlKey: true }))?.id).toBe('redo');
    expect(resolveShortcut(key('d', { metaKey: true }))?.id).toBe('duplicate');
    expect(resolveShortcut(key('s', { ctrlKey: true }))?.id).toBe('save');
  });

  test('leaves other browser combos alone', () => {
    for (const k of ['a', 'c', 'v', 'x', 'r', 'l', 'w', 't', '0', '=', '-', 'Backspace', 'ArrowLeft', 'Enter']) {
      expect(resolveShortcut(key(k, { metaKey: true }))).toBeNull();
      expect(resolveShortcut(key(k, { ctrlKey: true }))).toBeNull();
    }
    expect(resolveShortcut(key('ArrowLeft', { altKey: true }))).toBeNull();
    expect(resolveShortcut(key('a', { altKey: true }))).toBeNull();
    expect(resolveShortcut(key('q'))).toBeNull();
  });
});

describe('allowed', () => {
  const walk = resolveShortcut(key('ArrowRight')) as NonNullable<ReturnType<typeof resolveShortcut>>;
  const undo = resolveShortcut(key('z', { metaKey: true })) as typeof walk;
  const esc = resolveShortcut(key('Escape')) as typeof walk;
  const save = resolveShortcut(key('s', { metaKey: true })) as typeof walk;
  const open = resolveShortcut(key('Enter')) as typeof walk;

  const everywhere: Scopes = { canvas: () => true, editor: () => true, node: () => true };

  test('plain keys need the canvas or an unfocused page', () => {
    expect(allowed(walk, canvasEl, scopes)).toBe(true);
    expect(allowed(walk, el('BODY'), scopes)).toBe(true);
    expect(allowed(walk, el('MAIN', [], 'main-content'), scopes)).toBe(true);
    expect(allowed(walk, editorEl, scopes)).toBe(false);
    expect(allowed(walk, el('A'), scopes)).toBe(false);
  });

  test('typing targets block everything except Escape and Save', () => {
    const field = el('INPUT', ['input']);
    expect(allowed(walk, field, everywhere)).toBe(false);
    expect(allowed(undo, field, everywhere)).toBe(false);
    expect(allowed(esc, field, everywhere)).toBe(true);
    expect(allowed(save, field, everywhere)).toBe(true);
  });

  test('app chrome outside the editor is ignored', () => {
    expect(allowed(undo, el('DIV'), scopes)).toBe(false);
    expect(allowed(undo, editorEl, scopes)).toBe(true);
  });

  test('portaled overlays keep their own keys', () => {
    expect(allowed(esc, el('DIV', ['aria-modal']), everywhere)).toBe(false);
    expect(allowed(esc, el('DIV', ['bb-picker-panel']), everywhere)).toBe(false);
  });

  test('Enter opens a step only from the step itself', () => {
    expect(allowed(open, nodeEl, scopes)).toBe(true);
    expect(allowed(open, nodeButtonEl, scopes)).toBe(false);
    expect(allowed(open, canvasEl, scopes)).toBe(false);
  });
});

describe('neighbor', () => {
  test('right goes to the first output, left to the source', () => {
    expect(neighbor(flow, 't', 'right')).toBe('c');
    expect(neighbor(flow, 'c', 'right')).toBe('yes');
    expect(neighbor(flow, 'no', 'left')).toBe('c');
    expect(neighbor(flow, 't', 'left')).toBeNull();
    expect(neighbor(flow, 'yes', 'right')).toBeNull();
  });

  test('up and down walk the siblings that share a source', () => {
    expect(neighbor(flow, 'yes', 'down')).toBe('no');
    expect(neighbor(flow, 'no', 'up')).toBe('yes');
    expect(neighbor(flow, 'yes', 'up')).toBeNull();
    expect(neighbor(flow, 'no', 'down')).toBeNull();
  });

  test('unconnected steps fall back to their column', () => {
    expect(neighbor(flow, 'lone', 'down')).toBeNull();
    expect(neighbor(flow, 'ghost', 'right')).toBeNull();
  });
});

describe('neighborAfterRemove', () => {
  test('prefers what came next, then the source, then anything left', () => {
    expect(neighborAfterRemove(flow, 'c')).toBe('yes');
    expect(neighborAfterRemove(flow, 'no')).toBe('c');
    expect(neighborAfterRemove(flow, 'lone')).toBe('t');
    expect(neighborAfterRemove({ ...flow, nodes: [flow.nodes[0]], edges: [] }, 't')).toBeNull();
  });
});

describe('readingOrder, stepDelta, stepIndex', () => {
  test('reading order runs by column then row', () => {
    expect(readingOrder(flow).map((n) => n.id)).toEqual(['t', 'c', 'yes', 'no', 'lone']);
  });

  test('nudges move one grid cell', () => {
    expect(stepDelta('left')).toEqual({ dx: -24, dy: 0 });
    expect(stepDelta('down')).toEqual({ dx: 0, dy: 24 });
  });

  test('menu stepping wraps and honours Home and End', () => {
    expect(stepIndex(-1, 3, 'ArrowDown')).toBe(0);
    expect(stepIndex(2, 3, 'ArrowDown')).toBe(0);
    expect(stepIndex(0, 3, 'ArrowUp')).toBe(2);
    expect(stepIndex(1, 3, 'Home')).toBe(0);
    expect(stepIndex(1, 3, 'End')).toBe(2);
    expect(stepIndex(0, 0, 'ArrowDown')).toBe(-1);
  });
});
