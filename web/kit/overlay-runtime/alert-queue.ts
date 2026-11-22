// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AlertControl, FeedEvent } from './feed';
import type { AlertLook } from './widget-types';

export type AlertItem = { event: FeedEvent; look: AlertLook; priority: number };
type Active = AlertItem & { started: number; until: number };
type Options = { delay: number; threshold: number };

export class AlertQueue {
  #queued: AlertItem[] = [];
  #current: Active | undefined;
  #next = 0;
  #lastSkip = 0;
  #paused = false;
  #muted = false;
  #options: Options;

  constructor(options: Options) {
    this.#options = options;
  }

  get active(): Active | undefined {
    return this.#current;
  }

  get muted(): boolean {
    return this.#muted;
  }

  push(item: AlertItem, now: number): void {
    if (this.#interrupts(item)) this.#start(item, now);
    else this.#queued.push(item);
    this.tick(now);
  }

  apply(control: AlertControl, now: number): void {
    this.#paused = control.paused;
    this.#muted = control.muted;
    if (control.skip > this.#lastSkip) this.#finish(now);
    this.#lastSkip = control.skip;
    this.tick(now);
  }

  tick(now: number): void {
    const current = this.#current;
    if (current && now >= current.until) this.#finish(now);
    if (!this.#canStart(now)) return;
    const next = this.#queued.shift();
    if (next) this.#start(next, now);
  }

  #interrupts(item: AlertItem): boolean {
    const current = this.#current;
    if (!current || !this.#canInterrupt()) return false;
    return item.priority >= this.#options.threshold && item.priority > current.priority;
  }

  #canStart(now: number): boolean {
    return !this.#current && !this.#paused && now >= this.#next;
  }

  #canInterrupt(): boolean {
    return !this.#paused && this.#options.threshold > 0;
  }

  #finish(now: number): void {
    if (!this.#current) return;
    this.#current = undefined;
    this.#next = now + this.#options.delay * 1000;
  }

  #start(item: AlertItem, now: number): void {
    this.#current = { ...item, started: now, until: now + item.look.duration * 1000 };
  }
}
