// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { messageRuns } from './emotes';
import { applyFont, node, type Install } from './view';
import type { WidgetSettings } from './widget-types';

type EmoteSettings = WidgetSettings['emotewall'];

function floatingEmote(src: string, text: string, settings: EmoteSettings): HTMLImageElement {
  const img = node('img', `obs-flying-emote obs-emote-${settings.motion}`);
  img.src = src;
  img.alt = text;
  img.style.width = `${settings.size}px`;
  img.style.height = `${settings.size}px`;
  img.style.left = `${Math.round(Math.random() * 85)}%`;
  img.style.animationDuration = `${settings.lifetime}s`;
  return img;
}

type EmoteCount = { count: number; expires: number };

class EmoteCombo {
  #counts = new Map<string, EmoteCount>();
  #until = 0;

  constructor(
    private label: HTMLElement,
    private settings: EmoteSettings
  ) {}

  add(emote: HTMLImageElement, now: number): void {
    const previous = this.#counts.get(emote.src);
    const count = previous && previous.expires > now ? previous.count + 1 : 1;
    const expires = now + this.settings.lifetime * 1000;
    this.#counts.set(emote.src, { count, expires });
    if (count < this.settings.combo) return;
    this.#until = expires;
    this.label.textContent = `${emote.alt} x${count}`;
  }

  tick(now: number): void {
    for (const [id, value] of this.#counts) {
      if (now >= value.expires) this.#counts.delete(id);
    }
    if (now >= this.#until) this.label.textContent = '';
  }
}

export const installEmoteWall: Install<'emotewall'> = (host, settings, { assets }) => {
  const label = node('span', 'obs-emote-combo');
  const wall = node('div', 'obs-emote-wall');
  const combo = new EmoteCombo(label, settings);
  applyFont(label, settings.style, assets);
  host.append(wall, label);

  function add(img: HTMLImageElement, now: number): void {
    if (wall.childElementCount >= settings.max) wall.firstElementChild?.remove();
    wall.append(img);
    img.addEventListener('animationend', () => img.remove(), { once: true });
    combo.add(img, now);
  }

  return {
    event: (event, { now }) => {
      if (event.kind !== 'chat') return;
      for (const { text, url } of messageRuns(event.message)) {
        if (url) add(floatingEmote(url, text, settings), now);
      }
    },
    tick: ({ now }) => combo.tick(now)
  };
};
