// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Assets } from './assets';

type Track = { path: string; volume: number };

export class AlertAudio {
  #player?: HTMLAudioElement;
  #muted = false;

  constructor(private assets: Assets) {}

  start(sound: Track, muted: boolean): void {
    this.stop();
    this.#muted = muted;
    const asset = this.assets.resolve(sound.path);
    if (!asset || !sound.volume) return;
    const player = new Audio(asset.url);
    player.volume = sound.volume;
    player.muted = muted;
    this.#player = player;
    void player.play().catch(() => undefined);
  }

  mute(muted: boolean): void {
    this.#muted = muted;
    if (this.#player) this.#player.muted = muted;
  }

  stop(): void {
    this.#player?.pause();
    this.#player = undefined;
  }
}
