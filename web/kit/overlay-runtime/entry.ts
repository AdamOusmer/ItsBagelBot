// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { OverlayDocument } from '../lib/overlay-document';
import { eventKinds, type FeedMessages, type OverlayEvent } from './feed';
import { FeedState, MIN_CHAT, MIN_EVENTS } from './feed-state';
import { OverlayRuntime } from './runtime';

type Handlers = { [K in keyof FeedMessages]: (payload: FeedMessages[K]) => void };

const isEvent = (value: OverlayEvent): boolean => typeof value?.id === 'string' && (eventKinds as readonly string[]).includes(value.kind);

const largest = (doc: OverlayDocument, kind: string): number =>
  Math.max(0, ...doc.widgets.filter((widget) => widget.kind === kind).map((widget) => Number(widget.settings?.count) || 0));

const feedLimits = (doc: OverlayDocument) => ({
  events: Math.max(MIN_EVENTS, largest(doc, 'eventlist')),
  chat: Math.max(MIN_CHAT, largest(doc, 'chatbox'))
});

class GatewayOverlay {
  #runtime: OverlayRuntime;
  #feed = new FeedState();
  #doc?: Pick<OverlayDocument, 'width' | 'height'>;
  #handlers: Handlers = {
    event: (payload) => {
      if (!isEvent(payload)) return;
      const event = this.#feed.ingest(payload);
      this.#runtime.setSnapshot(this.#feed.snapshot);
      this.#runtime.event(event);
    },
    snapshot: (payload) => this.#apply(() => this.#feed.load(payload)),
    stats: (payload) => this.#apply(() => this.#feed.stats(payload)),
    credits: (payload) => this.#apply(() => this.#feed.credits(payload)),
    credits_replace: (payload) => this.#apply(() => this.#feed.replaceCredits(payload ?? {})),
    subathon: (payload) => this.#apply(() => this.#feed.subathon(payload)),
    control: (payload) => this.#apply(() => this.#feed.control(payload))
  };

  constructor(host: HTMLElement, sandboxOrigin: string) {
    this.#runtime = new OverlayRuntime(host, { sandboxOrigin });
    addEventListener('resize', this.#fit);
  }

  setDocument(doc: OverlayDocument): void {
    this.#doc = doc;
    this.#feed.limits = feedLimits(doc);
    this.#runtime.setDocument(doc);
    this.#fit();
  }

  feed<K extends keyof FeedMessages>(name: K, data: string): void {
    try {
      this.#handlers[name](JSON.parse(data) as FeedMessages[K]);
    } catch (error) {
      console.error('Overlay feed message failed', error);
    }
  }

  dispose(): void {
    removeEventListener('resize', this.#fit);
    this.#runtime.dispose();
  }

  #apply(change: () => void): void {
    change();
    this.#runtime.setSnapshot(this.#feed.snapshot);
  }

  #fit = (): void => {
    if (this.#doc) this.#runtime.scale(Math.min(innerWidth / this.#doc.width, innerHeight / this.#doc.height));
  };
}

declare global {
  interface Window {
    __overlayRuntime?: { mount: (host: HTMLElement, sandboxOrigin: string) => GatewayOverlay };
  }
}

window.__overlayRuntime = { mount: (host, sandboxOrigin) => new GatewayOverlay(host, sandboxOrigin) };
