// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const { moduleHrefOf, moduleStateOf } = await import('./module-view');

test('absent row follows the kind default, core is always on', () => {
  expect(moduleStateOf({ kind: 'default' }, undefined)).toBe('on');
  expect(moduleStateOf({ kind: 'optin' }, undefined)).toBe('off');
  expect(moduleStateOf({ kind: 'core' }, false)).toBe('core');
});

test('a stored row wins over the kind default', () => {
  expect(moduleStateOf({ kind: 'default' }, false)).toBe('off');
  expect(moduleStateOf({ kind: 'optin' }, true)).toBe('on');
});

test('href modules keep their own segment', () => {
  expect(moduleHrefOf('1', { id: 'tickets', href: 'tickets' })).toBe('/discord/1/tickets');
  expect(moduleHrefOf('1', { id: 'voice' })).toBe('/discord/1/modules/voice');
});
