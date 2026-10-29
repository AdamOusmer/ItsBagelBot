// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { field } from '../../ui/lib/light-field';
import { prefersReducedMotion } from '../../ui/lib/motion-query';
import { mountEngine } from '../../web/node_modules/@luzir/bolota/dist/engine.js';

const EXPRESSIONS = ['curious', 'surprised', 'shy', 'attentive'];
const EXPRESSION_MS = 3400;

function wakeMascot(svg: SVGSVGElement): void {
  if (prefersReducedMotion()) return;
  const handle = mountEngine(svg, svg.dataset.errorBolota ?? 'oven_fresh', {
    size: Number(svg.dataset.size) || 240,
    palette: { head: svg.dataset.head ?? '', eye: svg.dataset.eye ?? '' },
    background: false,
  });
  svg.querySelector('[data-pose]')?.setAttribute('display', 'none');
  handle.loop('idle');
  handle.follow('window');
  let step = 0;
  const next = () => handle.setExpression(EXPRESSIONS[step++ % EXPRESSIONS.length]);
  next();
  setInterval(next, EXPRESSION_MS);
}

type Copy = Record<string, string>;

function pickLocale(catalog: Record<string, Copy>): string | undefined {
  const wanted = navigator.languages?.length ? navigator.languages : [navigator.language];
  for (const tag of wanted.map((value) => value.toLowerCase())) {
    if (catalog[tag]) return tag;
    const base = tag.split('-')[0];
    const match = Object.keys(catalog).find((code) => code === base || code.startsWith(`${base}-`));
    if (match) return match;
  }
  return undefined;
}

function localize(scene: HTMLElement): void {
  const source = document.getElementById('error-copy');
  const catalog = JSON.parse(source?.textContent ?? '{}') as Record<string, Copy>;
  const locale = pickLocale(catalog);
  if (!locale || locale === 'en') return;
  const group = scene.dataset.group ?? '';
  document.documentElement.lang = locale;
  for (const node of scene.querySelectorAll<HTMLElement>('[data-copy]')) {
    const key = node.dataset.copy ?? '';
    const text = catalog[locale][key.startsWith('*') ? group + key.slice(1) : key];
    if (text) node.textContent = text;
  }
}

function wireBack(): void {
  const back = document.querySelector<HTMLAnchorElement>('[data-error-back]');
  back?.addEventListener('click', (event) => {
    if (window.history.length <= 1) return;
    event.preventDefault();
    window.history.back();
  });
}

const scene = document.querySelector<HTMLElement>('.bb-error-scene');
if (scene) localize(scene);
wireBack();
const host = document.querySelector<HTMLElement>('[data-field]');
if (host) field(host);
const mascot = document.querySelector<SVGSVGElement>('[data-error-bolota]');
if (mascot) wakeMascot(mascot);
