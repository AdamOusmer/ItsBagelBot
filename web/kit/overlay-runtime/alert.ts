// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { AlertAudio } from './alert-audio';
import { AlertQueue, type AlertItem } from './alert-queue';
import { messageNodes } from './emotes';
import { eventAmount, isAlert, renderTemplate, tokens } from './events';
import type { FeedEvent } from './feed';
import { GiftBurst } from './gift-burst';
import { selectVariation } from './variations';
import { applyFont, mediaElement, node, type Frame, type Install, type RenderContext } from './view';
import type { WidgetSettings } from './widget-types';

function animation(name: string, seconds: number, exit = false): string {
  if (name === 'none' || !seconds) return 'none';
  return `obs-${name} ${seconds}s ${exit ? 'ease-in reverse forwards' : 'ease-out both'}`;
}

function alertContent(item: AlertItem, context: RenderContext): HTMLElement {
  const { look, event } = item;
  const wrapper = node('div', `obs-alert obs-alert-${look.layout}`);
  const text = node('div', 'obs-alert-text', renderTemplate(look.title, tokens(event)));
  applyFont(text, look.style, context.assets);
  if (look.showMessage && event.message) {
    const message = node('div', 'obs-alert-message');
    message.append(...messageNodes(event.message));
    text.append(message);
  }
  text.style.visibility = look.textDelay ? 'hidden' : 'visible';
  const media = mediaElement(look.media, context.assets);
  if (media) wrapper.append(media);
  wrapper.append(text);
  return wrapper;
}

function reveal(element: HTMLElement, elapsed: number, delay: number): void {
  const text = element.querySelector<HTMLElement>('.obs-alert-text');
  if (text && elapsed >= delay * 1000) text.style.visibility = 'visible';
}

class AlertDisplay {
  #current?: AlertQueue['active'];
  #element?: HTMLElement;
  #audio: AlertAudio;
  #video?: HTMLVideoElement;
  #exiting = false;

  constructor(private host: HTMLElement, private context: RenderContext) {
    this.#audio = new AlertAudio(context.assets);
  }

  update(queue: AlertQueue, now: number): void {
    const active = queue.active;
    if (active !== this.#current) this.#show(active, queue.muted);
    this.#mute(queue.muted);
    if (active && this.#element) this.#animate(this.#element, active, now);
  }

  clear(): void {
    this.#audio.stop();
    this.#video?.pause();
    this.#element?.remove();
    this.#video = undefined;
    this.#element = undefined;
    this.#exiting = false;
  }

  #show(active: AlertQueue['active'], muted: boolean): void {
    this.clear();
    this.#current = active;
    if (!active) return;
    this.#element = alertContent(active, this.context);
    this.#element.style.animation = animation(active.look.enter, active.look.enterDuration);
    this.host.append(this.#element);
    this.#video = this.#element.querySelector('video') ?? undefined;
    this.#audio.start({ path: active.look.sound, volume: active.look.volume }, muted);
    this.#mute(muted);
    if (this.#video) this.#video.volume = active.look.volume;
    this.#video?.play().catch(() => undefined);
  }

  #mute(muted: boolean): void {
    this.#audio.mute(muted);
    if (this.#video) this.#video.muted = muted;
  }

  #animate(element: HTMLElement, active: NonNullable<AlertQueue['active']>, now: number): void {
    const { look } = active;
    reveal(element, now - active.started, look.textDelay);
    if (this.#exiting || look.exit === 'none') return;
    const exitDuration = Math.min(look.duration, look.exitDuration);
    if (now < active.until - exitDuration * 1000) return;
    element.style.animation = animation(look.exit, exitDuration, true);
    this.#exiting = true;
  }
}

function alertItem(event: FeedEvent, settings: WidgetSettings['alertbox']): AlertItem | undefined {
  if (!isAlert(event.kind)) return;
  const rule = settings.events[event.kind];
  if (!rule.enabled || eventAmount(event) < rule.minAmount) return;
  return { event, look: selectVariation(rule, event), priority: rule.priority };
}

export const installAlert: Install<'alertbox'> = (host, settings, context) => {
  const gifts = new GiftBurst();
  const queue = new AlertQueue({ delay: settings.queueDelay, threshold: settings.interruptThreshold });
  const display = new AlertDisplay(host, context);

  function enqueue(event: FeedEvent, now: number): void {
    const item = alertItem(event, settings);
    if (item) queue.push(item, now);
  }

  function update({ snapshot, now }: Frame, event?: FeedEvent): void {
    queue.apply(snapshot.control, now);
    const ready = event ? gifts.push(event, now) : gifts.flush(now);
    ready.forEach(item => enqueue(item, now));
    display.update(queue, now);
  }

  return {
    update,
    tick: update,
    event: (event, frame) => {
      if (!isAlert(event.kind)) return;
      update(frame, event);
    },
    dispose: () => display.clear(),
  };
};