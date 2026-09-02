// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// betaLocked is the one rule the tile grid and every write gate share, so a
// drift here would let a free channel toggle a beta module through a stale
// form while sesame silently drops it on the standard lane.
import { describe, expect, it } from 'bun:test';
import { betaLocked, type ModuleDef } from './module-def';

function def(partial: Partial<ModuleDef>): ModuleDef {
  return {
    id: 'x',
    label: 'X',
    tagline: '',
    description: '',
    icon: 'gem',
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
