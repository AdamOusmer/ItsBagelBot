// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Assets } from './assets';
import type { FeedEvent, FeedSnapshot } from './feed';
import { fontStack, loadFont } from './fonts';
import type { FontStyle, Widget } from './widget-types';

export type RenderContext = { assets: Assets; sandboxOrigin?: string; preview?: boolean };
export type Frame = { snapshot: FeedSnapshot; now: number };

export type View = {
  update?: (frame: Frame) => void;
  tick?: (frame: Frame) => void;
  event?: (event: FeedEvent, frame: Frame) => void;
  dispose?: () => void;
};
export type Install<K extends Widget['kind']> = (
  host: HTMLElement,
  settings: Extract<Widget, { kind: K }>['settings'],
  context: RenderContext & { id: string }
) => View | void;

export function node<K extends keyof HTMLElementTagNameMap>(type: K, className = '', text = ''): HTMLElementTagNameMap[K] {
  const element = document.createElement(type);
  element.className = className;
  element.textContent = text;
  return element;
}

export function applyFont(host: HTMLElement, style: FontStyle, assets: Assets): void {
  loadFont(style.family, assets);
  host.style.fontFamily = fontStack(style.family, assets);
  host.style.fontSize = `${style.size}px`;
  host.style.fontWeight = String(style.weight);
  host.style.color = style.color;
  host.style.setProperty('-webkit-text-stroke', `${style.stroke}px ${style.strokeColor}`);
  host.style.textShadow = style.shadow ? `0 1px ${style.shadow}px ${style.shadowColor}` : 'none';
}

export function mediaElement(ref: string, assets: Assets, fit = 'contain'): HTMLImageElement | HTMLVideoElement | undefined {
  const asset = assets.resolve(ref);
  if (!asset) return;
  const media = asset.type === 'webm' ? node('video', 'obs-media') : node('img', 'obs-media');
  media.src = asset.url;
  media.style.objectFit = fit;
  if (media instanceof HTMLVideoElement) {
    media.playsInline = true;
    media.preload = 'auto';
  } else {
    media.alt = '';
  }
  return media;
}

export type FrameReader<T> = (frame: Frame) => T;
type Draw = NonNullable<View['update']>;

export function timed(update: Draw, interval = 50): Pick<View, 'update' | 'tick'> {
  let next = 0;
  return {
    update,
    tick: (frame) => {
      if (frame.now < next) return;
      next = frame.now + interval;
      update(frame);
    }
  };
}

export function changed<T>(read: FrameReader<T>, render: (value: T) => void): NonNullable<View['update']> {
  let previous: string | undefined;
  let rendered = false;
  return (frame) => {
    const value = read(frame);
    const key = JSON.stringify(value);
    if (rendered && previous === key) return;
    rendered = true;
    previous = key;
    render(value);
  };
}
