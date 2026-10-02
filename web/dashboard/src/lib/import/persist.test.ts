// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { storage, useFakeSessionStorage } from '../../../test/session-storage';
import { loadSnapshot, saveSnapshot } from './persist';

useFakeSessionStorage();

const STAGES = ['pick', 'instructions', 'commands', 'extras', 'review', 'done'];
const SNAPSHOT_KEY = 'bb-welcome-import';

describe('import session snapshot', () => {
  test.each([
    ['nothing stored', null],
    ['text that is not JSON', 'not json'],
    ['JSON that is not an object', '[1]']
  ])('%s restores nothing', (_name, raw) => {
    if (raw !== null) storage.set(SNAPSHOT_KEY, raw);
    expect(loadSnapshot(STAGES)).toBeNull();
  });

  test('normalises unknown source and stage', () => {
    storage.set(SNAPSHOT_KEY, JSON.stringify({ source: 'nope', stage: 'zzz' }));
    expect(loadSnapshot(STAGES)).toMatchObject({ source: '', stage: 'pick', selected: {} });
  });

  test('keeps a valid review session', () => {
    storage.set(
      SNAPSHOT_KEY,
      JSON.stringify({ source: 'nightbot', stage: 'review', overwrite: true, previewResult: { stats: {}, manifest: {} }, selected: { 'commands:0': false } })
    );
    expect(loadSnapshot(STAGES)).toMatchObject({ source: 'nightbot', stage: 'review', overwrite: true, selected: { 'commands:0': false } });
  });

  test('a saved snapshot restores its selection and overwrite choice', () => {
    saveSnapshot({ source: 'nightbot', stage: 'review', overwrite: true, previewResult: null, commitResult: null, selected: { 'commands:1': true } });
    expect(loadSnapshot(STAGES)).toMatchObject({ source: 'nightbot', stage: 'review', overwrite: true, selected: { 'commands:1': true } });
  });
});
