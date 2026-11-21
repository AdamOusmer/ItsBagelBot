// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type SliderRange = { min: number; max: number; step: number; disabled?: boolean };

export type SliderHost = {
  range(): SliderRange;
  value(): number;
  input(value: number): void;
  change(value: number): void;
};

const KEY_STEPS: Record<string, number> = { ArrowRight: 1, ArrowUp: 1, ArrowLeft: -1, ArrowDown: -1, PageUp: 10, PageDown: -10 };

const clampUnit = (n: number): number => Math.min(1, Math.max(0, n));
const decimals = (n: number): number => (String(n).split('.')[1] ?? '').length;

export function snapValue(raw: number, { min, max, step }: SliderRange): number {
  const stepped = step > 0 ? min + Math.round((raw - min) / step) * step : raw;
  const exact = Number(stepped.toFixed(Math.max(decimals(step), decimals(min))));
  return Math.min(max, Math.max(min, exact));
}

export function sliderFraction(value: number, { min, max }: Pick<SliderRange, 'min' | 'max'>): number {
  return max > min ? clampUnit((Number(value) - min) / (max - min)) : 0;
}

export function pointerValue(clientX: number, rail: Pick<DOMRect, 'left' | 'width'>, range: SliderRange): number {
  const along = rail.width > 0 ? clampUnit((clientX - rail.left) / rail.width) : 0;
  return snapValue(range.min + along * (range.max - range.min), range);
}

export function keyValue(key: string, value: number, range: SliderRange): number | null {
  if (key === 'Home') return range.min;
  if (key === 'End') return range.max;
  const steps = KEY_STEPS[key];
  return steps === undefined ? null : snapValue(value + steps * (range.step || 1), range);
}

let labelCount = 0;

export function sliderLabelIds(thumb: HTMLElement): string | undefined {
  const byFor = thumb.id ? Array.from(document.querySelectorAll<HTMLLabelElement>(`label[for="${CSS.escape(thumb.id)}"]`)) : [];
  const wrapping = thumb.closest('label');
  const labels = new Set(wrapping ? [...byFor, wrapping] : byFor);
  const ids = Array.from(labels, (label) => {
    const text = label.querySelector<HTMLElement>('.bb-field__label, .bb-label') ?? label;
    text.id ||= `bb-slider-label-${++labelCount}`;
    return text.id;
  });
  return ids.length ? ids.join(' ') : undefined;
}

export function mountSlider(rail: HTMLElement, host: SliderHost): () => void {
  const thumb = rail.querySelector<HTMLElement>('[role="slider"]')!;
  let start = host.value();

  const move = (next: number): void => {
    if (next !== host.value()) host.input(next);
  };
  const settle = (): void => {
    const now = host.value();
    if (now !== start) host.change(now);
    start = now;
  };
  const follow = (event: PointerEvent): void => move(pointerValue(event.clientX, rail.getBoundingClientRect(), host.range()));

  const onDown = (event: PointerEvent): void => {
    if (host.range().disabled || event.button !== 0) return;
    event.preventDefault();
    start = host.value();
    thumb.setPointerCapture(event.pointerId);
    rail.classList.add('is-dragging');
    follow(event);
    thumb.focus({ preventScroll: true });
  };
  const onMove = (event: PointerEvent): void => {
    if (thumb.hasPointerCapture(event.pointerId)) follow(event);
  };
  const onRelease = (): void => {
    rail.classList.remove('is-dragging');
    settle();
  };
  const onKey = (event: KeyboardEvent): void => {
    if (host.range().disabled) return;
    const next = keyValue(event.key, host.value(), host.range());
    if (next === null) return;
    event.preventDefault();
    move(next);
  };

  rail.addEventListener('pointerdown', onDown);
  rail.addEventListener('pointermove', onMove);
  thumb.addEventListener('lostpointercapture', onRelease);
  thumb.addEventListener('keydown', onKey);
  thumb.addEventListener('keyup', settle);
  thumb.addEventListener('blur', settle);
  return () => {
    rail.removeEventListener('pointerdown', onDown);
    rail.removeEventListener('pointermove', onMove);
    thumb.removeEventListener('lostpointercapture', onRelease);
    thumb.removeEventListener('keydown', onKey);
    thumb.removeEventListener('keyup', settle);
    thumb.removeEventListener('blur', settle);
  };
}

function domHost(rail: HTMLElement): SliderHost {
  const thumb = rail.querySelector<HTMLElement>('[role="slider"]')!;
  const input = rail.querySelector<HTMLInputElement>('input');
  const read = (name: string): number => Number(thumb.getAttribute(name));
  const range = (): SliderRange => ({
    min: read('aria-valuemin'),
    max: read('aria-valuemax'),
    step: Number(rail.dataset.step) || 1,
    disabled: thumb.getAttribute('aria-disabled') === 'true',
  });
  const emit = (type: string): boolean => rail.dispatchEvent(new Event(type, { bubbles: true }));
  return {
    range,
    value: () => read('aria-valuenow'),
    input(value) {
      thumb.setAttribute('aria-valuenow', String(value));
      rail.style.setProperty('--slider-progress', String(sliderFraction(value, range())));
      if (input) input.value = String(value);
      emit('input');
    },
    change: () => emit('change'),
  };
}

export function enhanceSliders(root: ParentNode = document): void {
  for (const rail of root.querySelectorAll<HTMLElement>('.bb-slider:not([data-slider-ready])')) {
    rail.dataset.sliderReady = '';
    const thumb = rail.querySelector<HTMLElement>('[role="slider"]')!;
    const named = thumb.hasAttribute('aria-label') || thumb.hasAttribute('aria-labelledby');
    const ids = named ? undefined : sliderLabelIds(thumb);
    if (ids) thumb.setAttribute('aria-labelledby', ids);
    mountSlider(rail, domHost(rail));
  }
}
