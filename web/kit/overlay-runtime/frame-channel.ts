// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fieldValues, type WidgetBundle } from './bundle';
import type { FeedStats, OverlayEvent } from './feed';
import { isFrameMessage, type ParentMessage } from './frame-protocol';
import type { CustomValue } from './widget-types';

const MAX_PENDING = 100;

export class FrameChannel {
  #ready = false;
  #loaded = false;
  #bundle?: WidgetBundle;
  #session: FeedStats = {};
  #pending: OverlayEvent[] = [];
  #listen = (message: MessageEvent): void => {
    if (message.source !== this.frame.contentWindow || !isFrameMessage(message.data)) return;
    this.#ready = true;
    this.#flush();
  };

  constructor(
    private readonly frame: HTMLIFrameElement,
    private readonly values: Record<string, CustomValue>
  ) {
    addEventListener('message', this.#listen);
  }

  attach(bundle: WidgetBundle): void {
    this.#bundle = bundle;
    this.#flush();
  }

  event(event: OverlayEvent): void {
    if (this.#loaded) this.#post({ bagel: 1, type: 'event', event });
    else this.#pending = [...this.#pending.slice(-(MAX_PENDING - 1)), event];
  }

  session(session: FeedStats): void {
    this.#session = session;
    if (this.#loaded) this.#post({ bagel: 1, type: 'session', session });
  }

  dispose(): void {
    removeEventListener('message', this.#listen);
  }

  #flush(): void {
    const bundle = this.#loadable();
    if (!bundle) return;
    this.#loaded = true;
    const { fields, ...code } = bundle;
    this.#post({ bagel: 1, type: 'load', bundle: code, fields: fieldValues(fields, this.values), session: this.#session });
    this.#pending.forEach((event) => this.event(event));
    this.#pending = [];
  }

  #loadable(): WidgetBundle | undefined {
    return this.#ready && !this.#loaded ? this.#bundle : undefined;
  }

  #post(message: ParentMessage): void {
    this.frame.contentWindow?.postMessage(message, '*');
  }
}
