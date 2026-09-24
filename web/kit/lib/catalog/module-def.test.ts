// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, it } from 'bun:test';
import { betaLocked, type ModuleDef } from './module-def';

function def(partial: Partial<ModuleDef>): ModuleDef {
  return {
    id: 'x',
    label: 'X',
    tagline: '',
    description: '',
    category: 'c',
    defaultEnabled: false,
    replies: [],
    ...partial
  } as ModuleDef;
}

describe('betaLocked', () => {
  it('locks a beta module for a non-premium board', () => {
    expect(betaLocked(def({ beta: true }), false)).toBe(true);
  });
  it('opens a beta module for a premium board', () => {
    expect(betaLocked(def({ beta: true }), true)).toBe(false);
  });
  it('never locks a module that is not in beta', () => {
    expect(betaLocked(def({}), false)).toBe(false);
    expect(betaLocked(def({ beta: false }), false)).toBe(false);
  });
});
