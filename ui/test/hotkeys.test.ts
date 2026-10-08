// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';

import { isShortcut, isTyping, isTypingTarget } from '../lib/hotkeys';

const el = (tagName: string, isContentEditable = false) => ({ tagName, isContentEditable }) as unknown as EventTarget;

const key = (target: EventTarget | null, mods: Partial<Record<'altKey' | 'ctrlKey' | 'metaKey', boolean>> = {}) =>
  ({ target, altKey: false, ctrlKey: false, metaKey: false, ...mods }) as unknown as KeyboardEvent;

describe('hotkeys', () => {
  test('form fields and editable regions count as typing', () => {
    for (const tag of ['INPUT', 'TEXTAREA', 'SELECT']) expect(isTypingTarget(el(tag))).toBe(true);
    expect(isTypingTarget(el('DIV', true))).toBe(true);
  });

  test('everything else, and no target, does not', () => {
    expect(isTypingTarget(el('BUTTON'))).toBe(false);
    expect(isTypingTarget(el('svg'))).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
    expect(isTyping(key(el('A')))).toBe(false);
    expect(isTyping(key(el('INPUT')))).toBe(true);
  });

  test('a bare shortcut rejects typing and every modifier', () => {
    expect(isShortcut(key(el('BODY')))).toBe(true);
    expect(isShortcut(key(el('INPUT')))).toBe(false);
    expect(isShortcut(key(el('BODY'), { metaKey: true }))).toBe(false);
    expect(isShortcut(key(el('BODY'), { ctrlKey: true }))).toBe(false);
    expect(isShortcut(key(el('BODY'), { altKey: true }))).toBe(false);
  });

  test('an alt shortcut requires alt and still rejects ctrl, meta and typing', () => {
    expect(isShortcut(key(el('BODY'), { altKey: true }), { alt: true })).toBe(true);
    expect(isShortcut(key(el('BODY')), { alt: true })).toBe(false);
    expect(isShortcut(key(el('BODY'), { altKey: true, ctrlKey: true }), { alt: true })).toBe(false);
    expect(isShortcut(key(el('BODY'), { altKey: true, metaKey: true }), { alt: true })).toBe(false);
    expect(isShortcut(key(el('TEXTAREA'), { altKey: true }), { alt: true })).toBe(false);
  });
});
