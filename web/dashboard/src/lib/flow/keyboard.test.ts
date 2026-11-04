// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { describe, expect, test } from 'bun:test';
import type { Flow } from './model';
import { allowed, neighbor, neighborAfterRemove, readingOrder, resolveShortcut, stepDelta, stepIndex, type KeyLike, type Target } from './keyboard';

const key = (k: string, mods: Partial<KeyLike> = {}): KeyLike => ({ key: k, ctrlKey: false, metaKey: false, shiftKey: false, altKey: false, ...mods });

function el(tagName: string, within: string[] = [], id?: string): Target {
  return { tagName, id, closest: (selector: string) => (within.some((w) => selector.includes(w)) ? {} : null) };
}

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
  test('maps the documented keys', () => {
    expect(resolveShortcut(key('Delete'))?.id).toBe('delete');
    expect(resolveShortcut(key('Backspace'))?.id).toBe('delete');
    expect(resolveShortcut(key('Escape'))?.id).toBe('escape');
    expect(resolveShortcut(key('ArrowRight'))).toMatchObject({ id: 'walk', dir: 'right' });
    expect(resolveShortcut(key('ArrowUp', { shiftKey: true }))).toMatchObject({ id: 'nudge', dir: 'up' });
    expect(resolveShortcut(key('Enter'))?.id).toBe('open');
    expect(resolveShortcut(key(' '))?.id).toBe('open');
    expect(resolveShortcut(key('+', { shiftKey: true }))?.id).toBe('zoom-in');
    expect(resolveShortcut(key('='))?.id).toBe('zoom-in');
    expect(resolveShortcut(key('-'))?.id).toBe('zoom-out');
    expect(resolveShortcut(key('0'))?.id).toBe('fit');
    expect(resolveShortcut(key('a'))?.id).toBe('add');
    expect(resolveShortcut(key('A', { shiftKey: true }))?.id).toBe('add-other');
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

  test('plain keys need the canvas or an unfocused page', () => {
    expect(allowed(walk, el('DIV', ['data-flow-canvas']))).toBe(true);
    expect(allowed(walk, el('BODY'))).toBe(true);
    expect(allowed(walk, el('MAIN', [], 'main-content'))).toBe(true);
    expect(allowed(walk, el('BUTTON', ['data-flow-editor']))).toBe(false);
    expect(allowed(walk, el('A'))).toBe(false);
  });

  test('typing targets block everything except Escape and Save', () => {
    const field = el('INPUT', ['input', 'data-flow-editor', 'data-flow-canvas']);
    expect(allowed(walk, field)).toBe(false);
    expect(allowed(undo, field)).toBe(false);
    expect(allowed(esc, field)).toBe(true);
    expect(allowed(save, field)).toBe(true);
  });

  test('app chrome outside the editor is ignored', () => {
    expect(allowed(undo, el('DIV'))).toBe(false);
    expect(allowed(undo, el('DIV', ['data-flow-editor']))).toBe(true);
  });

  test('portaled overlays keep their own keys', () => {
    expect(allowed(esc, el('DIV', ['data-overlay', 'data-flow-editor']))).toBe(false);
  });

  test('Enter opens a step only from the step itself', () => {
    expect(allowed(open, el('DIV', ['data-node-id']))).toBe(true);
    expect(allowed(open, el('BUTTON', ['data-node-id', 'button']))).toBe(false);
    expect(allowed(open, el('DIV', ['data-flow-canvas']))).toBe(false);
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
