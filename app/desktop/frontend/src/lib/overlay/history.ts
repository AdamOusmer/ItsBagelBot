// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import type { Layer } from './types';

const MAX_ENTRIES = 50;

/** Linear undo/redo over full snapshots of the layer list. Simple, not diffed: fine at this scale. */
export class LayerHistory {
  #entries: Layer[][];
  #index = 0;

  constructor(initial: Layer[]) {
    this.#entries = [structuredClone(initial)];
  }

  commit(layers: Layer[]): void {
    this.#entries = this.#entries.slice(0, this.#index + 1);
    this.#entries.push(structuredClone(layers));
    if (this.#entries.length > MAX_ENTRIES) this.#entries.shift();
    this.#index = this.#entries.length - 1;
  }

  get canUndo(): boolean {
    return this.#index > 0;
  }

  get canRedo(): boolean {
    return this.#index < this.#entries.length - 1;
  }

  undo(): Layer[] | null {
    if (!this.canUndo) return null;
    this.#index -= 1;
    return structuredClone(this.#entries[this.#index]);
  }

  redo(): Layer[] | null {
    if (!this.canRedo) return null;
    this.#index += 1;
    return structuredClone(this.#entries[this.#index]);
  }
}
