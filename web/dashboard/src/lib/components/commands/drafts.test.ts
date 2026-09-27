// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeEach, describe, expect, test } from 'bun:test';
import { commandContentSnapshot, overlayLiveActive } from '../../../../../kit/lib/command-active';
import { draftKey, loadDraft, type CommandDraft } from './drafts';

const storage = new Map<string, string>();
const originalStorage = Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage');
Object.defineProperty(globalThis, 'sessionStorage', {
  configurable: true,
  value: { getItem: (key: string) => storage.get(key) ?? null }
});
beforeEach(() => storage.clear());
afterAll(() => {
  if (originalStorage) Object.defineProperty(globalThis, 'sessionStorage', originalStorage);
  else Reflect.deleteProperty(globalThis, 'sessionStorage');
});

const draft: CommandDraft = {
  edit: false,
  originalName: '',
  name: 'hello',
  aliases: ['hi'],
  response: 'Hello {user}!\nWelcome back.',
  perm: 'everyone',
  cooldown: 0,
  allowed_user_id: '',
  bump_counter: '',
  stream_online_only: false,
  is_active: true
};

describe('command draft restoration', () => {
  test('repairs old content snapshots so New can mount after typing and reloading', () => {
    // This is the exact format the old CommandEditor wrote after each change.
    storage.set(draftKey('', false), commandContentSnapshot(draft));
    const restored = loadDraft('', false);
    expect(restored).toEqual(draft);
    expect(typeof restored?.is_active).toBe('boolean');
  });

  test('preserves a new command explicitly disabled before a reload', () => {
    const disabled = { ...draft, is_active: false, stream_online_only: true };
    storage.set(draftKey('', false), JSON.stringify(disabled));
    expect(loadDraft('', false)).toEqual(disabled);
  });

  test('the requested edit identity and live Active override stored metadata', () => {
    storage.set(draftKey('hello', true), JSON.stringify({ ...draft, originalName: 'wrong', builtin: true }));
    const restored = loadDraft('hello', true)!;
    expect(restored.edit).toBe(true);
    expect(restored.originalName).toBe('hello');
    expect(restored.builtin).toBeUndefined();
    expect(overlayLiveActive(restored, false).is_active).toBe(false);
    expect(restored.response).toBe(draft.response);
  });

  test('keeps invalid command content editable rather than silently discarding it', () => {
    const invalid = { ...draft, name: '!', response: '', cooldown: -2, aliases: ['!', '!'] };
    storage.set(draftKey('', false), JSON.stringify(invalid));
    expect(loadDraft('', false)).toEqual(invalid);
  });

  test('rejects invalid JSON and incompatible field types without throwing', () => {
    for (const raw of ['{', 'null', '[]', '42', JSON.stringify({ ...draft, response: null }),
      JSON.stringify({ ...draft, aliases: ['hi', 3] }), JSON.stringify({ ...draft, cooldown: '4' }),
      JSON.stringify({ ...draft, is_active: 'on' })]) {
      storage.set(draftKey('', false), raw);
      expect(loadDraft('', false)).toBeNull();
    }
  });

  test('fills missing editor fields in older snapshots', () => {
    storage.set(draftKey('', false), JSON.stringify({ name: 'hello', response: 'keep my text' }));
    expect(loadDraft('', false)).toEqual({ ...draft, aliases: [], response: 'keep my text' });
  });

  test('restores drafts from before the counter field existed', () => {
    const { bump_counter, ...legacy } = draft;
    storage.set(draftKey('', false), JSON.stringify(legacy));
    expect(loadDraft('', false)?.bump_counter).toBe('');
    expect(loadDraft('', false)?.response).toBe(draft.response);
  });

  test('preserves a saved counter selection', () => {
    const counted = { ...draft, bump_counter: 'deaths' };
    storage.set(draftKey('', false), JSON.stringify(counted));
    expect(loadDraft('', false)).toEqual(counted);
  });

  test('a missing draft and unavailable storage are harmless', () => {
    expect(loadDraft('', false)).toBeNull();
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      get() { throw new Error('storage unavailable'); }
    });
    expect(loadDraft('', false)).toBeNull();
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      value: { getItem: (key: string) => storage.get(key) ?? null }
    });
  });
});
