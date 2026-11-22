// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const MAX_ENTRIES = 50;

export class LayerHistory<T> {
  #entries: T[];
  #index = 0;

  constructor(initial: T) {
    this.#entries = [structuredClone(initial)];
  }

  get current(): T {
    return structuredClone(this.#entries[this.#index]!);
  }

  get canUndo(): boolean {
    return this.#index > 0;
  }

  get canRedo(): boolean {
    return this.#index < this.#entries.length - 1;
  }

  commit(value: T): void {
    this.#entries = this.#entries.slice(0, this.#index + 1);
    this.#entries.push(structuredClone(value));
    if (this.#entries.length > MAX_ENTRIES) this.#entries.shift();
    this.#index = this.#entries.length - 1;
  }

  step(direction: -1 | 1): T | undefined {
    const next = this.#index + direction;
    if (next < 0 || next >= this.#entries.length) return undefined;
    this.#index = next;
    return structuredClone(this.#entries[next]!);
  }
}
