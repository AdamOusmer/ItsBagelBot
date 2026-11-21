// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export class ReadOrder {
  private latestTicket = 0;
  private minimumTicket = -1;

  start(): number {
    return ++this.latestTicket;
  }

  invalidate(): void {
    this.minimumTicket = ++this.latestTicket;
  }

  current(ticket: number): boolean {
    return ticket >= this.minimumTicket;
  }

  accept(ticket: number): boolean {
    if (!this.current(ticket)) return false;
    this.minimumTicket = ticket;
    return true;
  }
}
