// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { displayName } from './events';
import type { AlertKind, FeedStats, Subathon } from './feed';
import { clock, remaining } from './time';
import { applyFont, changed, mediaElement, node, timed, type Install } from './view';
import type { WidgetSettings } from './widget-types';

export const installText: Install<'text'> = (host, settings, { assets }) => {
  applyFont(host, settings.style, assets);
  host.classList.add('obs-text');
  host.style.textAlign = settings.align;
  host.textContent = settings.text;
};

export const installImage: Install<'image'> = (host, settings, { assets }) => {
  const img = mediaElement(settings.media, assets, settings.fit);
  if (img instanceof HTMLImageElement) host.append(img);
};

export const installVideo: Install<'video'> = (host, settings, { assets, preview }) => {
  const video = mediaElement(settings.media, assets, settings.fit);
  if (!(video instanceof HTMLVideoElement)) return;
  video.loop = settings.loop;
  video.muted = settings.muted || Boolean(preview);
  video.autoplay = true;
  host.append(video);
  return { dispose: () => video.pause() };
};

export const installSlideshow: Install<'slideshow'> = (host, settings, { assets }) => {
  const images = settings.images.flatMap(path => {
    const img = mediaElement(path, assets, settings.fit);
    if (!(img instanceof HTMLImageElement)) return [];
    img.classList.add('obs-slide');
    img.style.transitionDuration = `${Math.min(settings.fade, settings.interval)}s`;
    return [img];
  });
  host.append(...images);
  let started: number | undefined;
  return timed(changed(({ now }) => {
    started ??= now;
    return Math.floor((now - started) / (settings.interval * 1000)) % (images.length || 1);
  }, index => images.forEach((img, i) => img.classList.toggle('obs-slide-active', i === index))));
};

type Timer = WidgetSettings['timer'];
function timerText(settings: Timer, state: Subathon, now: number): string {
  if (settings.mode === 'subathon') return clock(remaining(state, now), settings.format);
  const at = Date.parse(settings.at);
  if (!Number.isFinite(at)) return clock(0, settings.format);
  if (settings.mode === 'countup') return clock(Math.max(0, Math.floor((now - at) / 1000)), settings.format);
  const seconds = Math.ceil((at - now) / 1000);
  return seconds <= 0 ? settings.endText : clock(seconds, settings.format);
}

export const installTimer: Install<'timer'> = (host, settings, { assets }) => {
  applyFont(host, settings.style, assets);
  host.classList.add('obs-timer');
  const draw = changed(({ snapshot, now }) => timerText(settings, snapshot.subathon, now), text => { host.textContent = text; });
  return timed(draw, 200);
};

type CreditsKey = keyof WidgetSettings['credits']['headers'];
const creditKinds: Record<CreditsKey, AlertKind[]> = {
  follow: ['follow'], sub: ['sub', 'resub'], gift_sub: ['gift_sub', 'community_gift'],
  cheer: ['cheer'], raid: ['raid'], redemption: ['redemption'],
};

function credits(value: FeedStats['credits'], headers: WidgetSettings['credits']['headers']): HTMLElement[] {
  return (Object.entries(headers) as [CreditsKey, string][]).flatMap(([key, title]) => {
    const names = new Set(creditKinds[key].flatMap(kind => (value?.[kind] ?? []).map(displayName)));
    if (!names.size) return [];
    const group = node('section', 'obs-credit-group');
    group.append(node('strong', '', title), ...[...names].map(name => node('div', '', name)));
    return [group];
  });
}

function animateCredits(roll: HTMLElement, start: number, speed: number, onFinish: () => void): Animation | undefined {
  if (!roll.childElementCount) return;
  const end = roll.offsetHeight;
  const animation = roll.animate(
    [{ transform: `translateY(${start}px)` }, { transform: `translateY(-${end}px)` }],
    { duration: 1000 * (start + end) / speed, fill: 'forwards' },
  );
  animation.onfinish = () => { if (animation.playState === 'finished') onFinish(); };
  return animation;
}

export const installCredits: Install<'credits'> = (host, settings, { assets }) => {
  applyFont(host, settings.style, assets);
  const roll = node('div', 'obs-credit-roll');
  host.append(roll);
  let animation: Animation | undefined;
  let latest: FeedStats['credits'];
  let pending = false;

  function begin(): void {
    animation?.cancel();
    if (pending) roll.replaceChildren(...credits(latest, settings.headers));
    pending = false;
    animation = animateCredits(roll, host.clientHeight, settings.speed, () => {
      if (pending || settings.loop) begin();
    });
  }

  return { update: changed(({ snapshot }) => snapshot.stats.credits, value => {
    latest = value;
    pending = true;
    if (animation?.playState !== 'running') begin();
  }), dispose: () => animation?.cancel() };
};