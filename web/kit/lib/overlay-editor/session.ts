// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayDocument } from '../overlay-document';
import { LayerHistory } from './history';

export class EditorSession {
  #history: LayerHistory<OverlayDocument>;
  #committed: OverlayDocument;
  saved: OverlayDocument;

  constructor(initial: OverlayDocument) {
    this.#history = new LayerHistory(initial);
    this.#committed = initial;
    this.saved = initial;
  }

  get canUndo(): boolean {
    return this.#history.canUndo;
  }

  get canRedo(): boolean {
    return this.#history.canRedo;
  }

  commit(doc: OverlayDocument): void {
    if (doc === this.#committed) return;
    this.#committed = doc;
    this.#history.commit(doc);
  }

  revert(): OverlayDocument {
    return this.#committed;
  }

  step(direction: -1 | 1): OverlayDocument | undefined {
    const doc = this.#history.step(direction);
    if (!doc) return undefined;
    this.#committed = { ...doc, rev: this.saved.rev };
    return this.#committed;
  }

  markSaved(doc: OverlayDocument): void {
    this.saved = doc;
    this.#committed = { ...this.#committed, rev: doc.rev };
  }
}
