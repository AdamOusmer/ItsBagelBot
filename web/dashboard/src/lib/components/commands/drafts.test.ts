// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeEach, describe, expect, test } from 'bun:test';
import { commandContentSnapshot, overlayLiveActive } from '../../../../../kit/lib/command-active';
import { discardLegacyDrafts, draftKey, hasDraft, loadDraft, saveDraft, type CommandDraft, type DraftRef } from './drafts';

const storage = new Map<string, string>();
const fakeStorage = {
  get length() { return storage.size; },
  key: (i: number) => [...storage.keys()][i] ?? null,
  getItem: (key: string) => storage.get(key) ?? null,
  setItem: (key: string, value: string) => void storage.set(key, value),
  removeItem: (key: string) => void storage.delete(key)
};
const originalStorage = Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage');
Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, value: fakeStorage });
const BOARD = '1001';
const NEW: DraftRef = { board: BOARD, name: '', edit: false };
const HELLO: DraftRef = { board: BOARD, name: 'hello', edit: true };
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
  user_cooldown: 0,
  allowed_user_id: '',
  bump_counter: '',
  stream_online_only: false,
  is_active: true
};

describe('command draft restoration', () => {
  test('repairs old content snapshots so New can mount after typing and reloading', () => {
    // This is the exact format the old CommandEditor wrote after each change.
    storage.set(draftKey(NEW), commandContentSnapshot(draft));
    const restored = loadDraft(NEW);
    expect(restored).toEqual(draft);
    expect(typeof restored?.is_active).toBe('boolean');
  });

  test('preserves a new command explicitly disabled before a reload', () => {
    const disabled = { ...draft, is_active: false, stream_online_only: true };
    storage.set(draftKey(NEW), JSON.stringify(disabled));
    expect(loadDraft(NEW)).toEqual(disabled);
  });

  test('the requested edit identity and live Active override stored metadata', () => {
    storage.set(draftKey(HELLO), JSON.stringify({ ...draft, originalName: 'wrong', builtin: true }));
    const restored = loadDraft(HELLO)!;
    expect(restored.edit).toBe(true);
    expect(restored.originalName).toBe('hello');
    expect(restored.builtin).toBeUndefined();
    expect(overlayLiveActive(restored, false).is_active).toBe(false);
    expect(restored.response).toBe(draft.response);
  });

  test('keeps invalid command content editable rather than silently discarding it', () => {
    const invalid = { ...draft, name: '!', response: '', cooldown: -2, aliases: ['!', '!'] };
    storage.set(draftKey(NEW), JSON.stringify(invalid));
    expect(loadDraft(NEW)).toEqual(invalid);
  });

  test('rejects invalid JSON and incompatible field types without throwing', () => {
    for (const raw of ['{', 'null', '[]', '42', JSON.stringify({ ...draft, response: null }),
      JSON.stringify({ ...draft, aliases: ['hi', 3] }), JSON.stringify({ ...draft, cooldown: '4' }),
      JSON.stringify({ ...draft, user_cooldown: '60' }),
      JSON.stringify({ ...draft, is_active: 'on' })]) {
      storage.set(draftKey(NEW), raw);
      expect(loadDraft(NEW)).toBeNull();
    }
  });

  test('fills missing editor fields in older snapshots', () => {
    storage.set(draftKey(NEW), JSON.stringify({ name: 'hello', response: 'keep my text' }));
    expect(loadDraft(NEW)).toEqual({ ...draft, aliases: [], response: 'keep my text' });
  });

  test('restores drafts from before the counter and per-viewer cooldown fields existed', () => {
    const { bump_counter, user_cooldown, ...legacy } = draft;
    storage.set(draftKey(NEW), JSON.stringify(legacy));
    expect(loadDraft(NEW)?.bump_counter).toBe('');
    expect(loadDraft(NEW)?.user_cooldown).toBe(0);
    expect(loadDraft(NEW)?.response).toBe(draft.response);
  });

  test('preserves a saved counter selection and per-viewer cooldown', () => {
    const counted = { ...draft, bump_counter: 'deaths', user_cooldown: 60 };
    storage.set(draftKey(NEW), JSON.stringify(counted));
    expect(loadDraft(NEW)).toEqual(counted);
  });

  test('a missing draft and unavailable storage are harmless', () => {
    expect(loadDraft(NEW)).toBeNull();
    Object.defineProperty(globalThis, 'sessionStorage', {
      configurable: true,
      get() { throw new Error('storage unavailable'); }
    });
    expect(loadDraft(NEW)).toBeNull();
    Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, value: fakeStorage });
  });
});

describe('command draft channel scope', () => {
  test('a draft typed on one board never restores or badges on another', () => {
    storage.set(draftKey(NEW), JSON.stringify(draft));
    storage.set(draftKey(HELLO), JSON.stringify(draft));
    expect(loadDraft({ ...NEW, board: '2002' })).toBeNull();
    expect(loadDraft({ ...HELLO, board: '2002' })).toBeNull();
    expect(hasDraft({ ...HELLO, board: '2002' })).toBe(false);
    expect(hasDraft(HELLO)).toBe(true);
  });

  test('unscoped drafts from before channel scoping are discarded, never restored', () => {
    storage.set('bb-cmd-draft:new', JSON.stringify(draft));
    storage.set('bb-cmd-draft:hello', JSON.stringify(draft));
    storage.set(draftKey(HELLO), JSON.stringify(draft));
    storage.set('unrelated', 'keep');
    expect(loadDraft(NEW)).toBeNull();
    discardLegacyDrafts();
    expect([...storage.keys()].sort()).toEqual([draftKey(HELLO), 'unrelated'].sort());
  });

  test('reverting an edit restores the draft that was stored when the editor opened', () => {
    const opened = loadDraft(HELLO);
    saveDraft(HELLO, draft);
    saveDraft(HELLO, opened);
    expect(storage.has(draftKey(HELLO))).toBe(false);

    storage.set(draftKey(HELLO), JSON.stringify(draft));
    const restored = loadDraft(HELLO);
    saveDraft(HELLO, { ...draft, response: 'changed' });
    saveDraft(HELLO, restored);
    expect(loadDraft(HELLO)?.response).toBe(draft.response);
  });

  test('an unreadable stored draft is not carried forward on revert', () => {
    storage.set(draftKey(NEW), '{');
    expect(loadDraft(NEW)).toBeNull();
});
});
