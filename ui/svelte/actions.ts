// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Action } from 'svelte/action';

import { observeDecode, type DecodeOptions } from '../lib/decode';
import { observeReveal, type RevealOptions } from '../lib/reveal';
import { mountMagnetic, type MagneticOptions } from '../lib/magnetic';

export const reveal: Action<HTMLElement, RevealOptions | undefined> = (node, options) => {
  const dispose = observeReveal(node, options);
  return { destroy: dispose };
};

export const decode: Action<HTMLElement, DecodeOptions | undefined> = (node, options) => {
  const dispose = observeDecode(node, options);
  return { destroy: dispose };
};

export const magnetic: Action<HTMLElement, MagneticOptions | undefined> = (node, options) => {
  const dispose = mountMagnetic(node, options);
  return { destroy: dispose };
};
