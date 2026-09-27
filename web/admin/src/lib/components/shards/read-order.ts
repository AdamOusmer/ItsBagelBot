// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/** Dispatching a slow read must not hide an already available initial result.
 * Only a usable completed response makes older responses stale. */
export class ReadOrder {
  private issued = 0;
  private applied = -1;

  start(): number {
    return ++this.issued;
  }

  // A mutation makes every read dispatched before it stale, even if no
  // newer read has finished yet.
  invalidate(): void {
    this.applied = ++this.issued;
  }

  current(ticket: number): boolean {
    return ticket >= this.applied;
  }

  accept(ticket: number): boolean {
    if (!this.current(ticket)) return false;
    this.applied = ticket;
    return true;
  }
}
