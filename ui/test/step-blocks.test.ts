// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';
import SvelteStepper from '../svelte/Stepper.svelte';
import AstroStepper from '../astro/Stepper.astro';
import SvelteStepList from '../svelte/StepList.svelte';
import AstroStepList from '../astro/StepList.astro';
import AstroStepListDetail from './fixtures/step-list-detail.astro';
import SvelteSpinner from '../svelte/Spinner.svelte';
import AstroSpinner from '../astro/Spinner.astro';
import SvelteLineSeries from '../svelte/LineSeries.svelte';
import AstroLineSeries from '../astro/LineSeries.astro';
import { createRawSnippet, type Component } from 'svelte';

const container = await experimental_AstroContainer.create();
const svelte = (component: unknown, props: Record<string, unknown>) =>
  normalise(render(component as Component<Record<string, unknown>>, { props }).body);
const astro = async (component: Parameters<typeof container.renderToString>[0], props: Record<string, unknown>) =>
  normalise(await container.renderToString(component, { props }));

const WIZARD = [{ label: 'Welcome' }, { label: 'App', detail: 'Create it' }, { label: 'Keys' }];

const pip = '<span class="bb-stepper__pip" aria-hidden="true"></span>';
const check =
  '<svg class="bb-icon" viewBox="0 0 24 24" width="11" height="11" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6L9 17l-5-5"></svg>';

describe('Stepper', () => {
  test('horizontal with onselect: numbered buttons, current marked, steps past maxStep disabled', () => {
    const html = svelte(SvelteStepper, { steps: WIZARD, current: 1, maxStep: 1, label: 'Step 2 of 3', onselect: () => {} });
    expect(html).toBe(
      '<nav class="bb-stepper" aria-label="Step 2 of 3" style="--stepper-i: 1; --stepper-n: 3;">' +
        '<span class="bb-stepper__track" aria-hidden="true"><span class="bb-stepper__trail"></span></span>' +
        '<ol class="bb-stepper__list">' +
        `<li class="bb-stepper__item bb-stepper__item--done"><button type="button" class="bb-stepper__step" aria-label="Welcome"><span class="bb-stepper__num">01</span>${pip}</button></li>` +
        `<li class="bb-stepper__item bb-stepper__item--current"><button type="button" class="bb-stepper__step" aria-label="App" aria-current="step"><span class="bb-stepper__num">02</span>${pip}</button></li>` +
        `<li class="bb-stepper__item"><button type="button" class="bb-stepper__step" aria-label="Keys" disabled><span class="bb-stepper__num">03</span>${pip}</button></li>` +
        '</ol><span class="bb-stepper__glide" aria-hidden="true"></span></nav>',
    );
  });

  test('a negative current idles the trail and hides the glide', () => {
    const html = svelte(SvelteStepper, { steps: WIZARD, current: -1, label: 'Steps', onselect: () => {} });
    expect(html).toContain('style="--stepper-i: 0; --stepper-n: 3;"');
    expect(html).toContain('class="bb-stepper__track bb-stepper__track--idle"');
    expect(html).toContain('class="bb-stepper__glide bb-stepper__glide--hidden"');
    expect(html).not.toContain('aria-current');
  });

  test('horizontal without onselect is a static indicator, identical in both adapters', async () => {
    const props = { steps: WIZARD, current: 2, label: 'Step 3 of 3' };
    const html = svelte(SvelteStepper, props);
    expect(html).toContain('<span class="bb-stepper__step" role="img" aria-label="Keys" aria-current="step">');
    expect(html).not.toContain('<button');
    expect(await astro(AstroStepper, props)).toBe(html);
  });

  test('vertical: check on done steps, numbers after, connector on all but the last, detail when given', async () => {
    const props = { steps: WIZARD, current: 1, label: 'Stages', orientation: 'vertical' };
    const html = svelte(SvelteStepper, props);
    expect(html).toBe(
      '<ol class="bb-stepper bb-stepper--vertical" aria-label="Stages">' +
        `<li class="bb-stepper__item bb-stepper__item--done"><span class="bb-stepper__gutter" aria-hidden="true"><span class="bb-stepper__dot">${check}</span><span class="bb-stepper__bar"></span></span>` +
        '<span class="bb-stepper__text"><span class="bb-stepper__title">Welcome</span></span></li>' +
        '<li class="bb-stepper__item bb-stepper__item--current" aria-current="step"><span class="bb-stepper__gutter" aria-hidden="true"><span class="bb-stepper__dot">2</span><span class="bb-stepper__bar"></span></span>' +
        '<span class="bb-stepper__text"><span class="bb-stepper__title">App</span><span class="bb-stepper__detail">Create it</span></span></li>' +
        '<li class="bb-stepper__item"><span class="bb-stepper__gutter" aria-hidden="true"><span class="bb-stepper__dot">3</span></span>' +
        '<span class="bb-stepper__text"><span class="bb-stepper__title">Keys</span></span></li></ol>',
    );
    expect(await astro(AstroStepper, props)).toBe(html);
  });

  test('compact adds the modifier and the current label after the glide, hidden from assistive tech', () => {
    const html = svelte(SvelteStepper, { steps: WIZARD, current: 1, maxStep: 1, label: 'Step 2 of 3', compact: true, onselect: () => {} });
    expect(html.startsWith('<nav class="bb-stepper bb-stepper--compact" aria-label="Step 2 of 3"')).toBe(true);
    expect(html.endsWith('<span class="bb-stepper__glide" aria-hidden="true"></span><span class="bb-stepper__compact" aria-hidden="true">App</span></nav>')).toBe(true);
  });

  test('compact is opt-in: false renders exactly the default rail', () => {
    const props = { steps: WIZARD, current: 1, label: 'Step 2 of 3', onselect: () => {} };
    const html = svelte(SvelteStepper, { ...props, compact: false });
    expect(html).toBe(svelte(SvelteStepper, props));
    expect(html).not.toContain('compact');
  });

  test('compact renders the same in both adapters, with an empty label before the first step', async () => {
    for (const current of [2, -1]) {
      const props = { steps: WIZARD, current, label: 'Steps', compact: true };
      expect(await astro(AstroStepper, props)).toBe(svelte(SvelteStepper, props));
    }
    expect(svelte(SvelteStepper, { steps: WIZARD, current: -1, label: 'Steps', compact: true })).toContain(
      '<span class="bb-stepper__compact" aria-hidden="true"></span>',
    );
  });

  test('compact CSS: the label reserves 44px in pale tan mono caps and replaces the cells at 760px', async () => {
    const css = await Bun.file(new URL('../styles/elements/stepper.css', import.meta.url)).text();
    expect(css).toMatch(
      /\.bb-stepper__compact \{\s*display: none;\s*align-items: center;\s*min-height: 44px;\s*color: var\(--bb-tan-pale\);\s*font-family: var\(--bb-font-mono\);\s*font-size: 11px;\s*letter-spacing: 0\.1em;\s*text-transform: uppercase;\s*\}/,
    );
    expect(css).toMatch(
      /@media \(max-width: 760px\) \{[^@]*\.bb-stepper--compact :is\(\.bb-stepper__list, \.bb-stepper__track, \.bb-stepper__glide\) \{\s*display: none;\s*\}\s*\.bb-stepper__compact \{\s*display: flex;\s*\}/,
    );
  });
});

const CHECKLIST = [
  { id: 'tag', label: 'Tag', state: 'succeeded', value: 1, meta: '1/1' },
  { id: 'build', label: 'Build', state: 'running', value: null, meta: '3/6 jobs' },
  { id: 'roll', label: 'Rollout', state: 'failed', value: 0.5, disabled: true },
  { id: 'verify', label: 'Verify', state: 'pending' },
];

describe('StepList navigable form', () => {
  const html = svelte(SvelteStepList, { steps: CHECKLIST, selected: 'build', label: 'Stages', onselect: () => {} });

  test('onselect turns the list into a labelled nav with a glide at the selected index', () => {
    expect(html.startsWith('<nav class="bb-steps bb-steps--nav" aria-label="Stages" style="--steps-at: 1; --steps-n: 4;">')).toBe(true);
    expect(html).toContain('<span class="bb-steps__glide" aria-hidden="true"></span><ol class="bb-steps__list">');
  });

  test('rows are buttons: selected is current, disabled rows are disabled, running is not current by itself', () => {
    const rows = [...html.matchAll(/<button type="button" class="bb-step__row"([^>]*)>/g)].map((m) => m[1].trim());
    expect(rows).toEqual(['', 'aria-current="step"', 'disabled', '']);
  });

  test('marks: check for succeeded, x for failed, zero-padded number otherwise', () => {
    const marks = [...html.matchAll(/class="bb-step__mark" aria-hidden="true">(<svg[^>]*><path d="([^"]+)"|<span class="bb-step__num">(\d+))/g)].map(
      (m) => m[2] ?? m[3],
    );
    expect(marks).toEqual(['M20 6L9 17l-5-5', '02', 'M18 6L6 18M6 6l12 12', '04']);
  });

  test('track fills with the value; null is indeterminate; meta always renders', () => {
    const fills = [...html.matchAll(/<span class="(bb-step__fill[^"]*)" style="--step-value: ([^;]+);">/g)].map((m) => `${m[1]}|${m[2]}`);
    expect(fills).toEqual(['bb-step__fill|1', 'bb-step__fill bb-step__fill--indeterminate|0', 'bb-step__fill|0.5', 'bb-step__fill|0']);
    expect(html).toContain('<span class="bb-step__meta"></span>');
  });

  test('an unknown selection parks the glide on the first row', () => {
    const other = svelte(SvelteStepList, { steps: CHECKLIST, selected: 'nope', onselect: () => {} });
    expect(other).toContain('style="--steps-at: 0; --steps-n: 4;"');
    expect(other).not.toContain('aria-current');
  });
});

describe('StepList Astro adapter', () => {
  const LIVE = [
    { id: 'tag', label: 'Tag', state: 'succeeded', value: 1, href: '/runs/1#tag' },
    { id: 'build', label: 'Build', state: 'running', value: null, meta: '3/6 jobs' },
    { id: 'roll', label: 'Rollout', state: 'failed', value: Number.NaN },
    { id: 'verify', label: 'Verify', state: 'waiting' },
  ];

  test('renders the live list exactly like the Svelte adapter', async () => {
    expect(await astro(AstroStepList, { steps: LIVE, stateLabels: { running: 'En cours' } })).toBe(
      svelte(SvelteStepList, { steps: LIVE, stateLabels: { running: 'En cours' } }),
    );
  });

  test('a function child renders per-row detail like the Svelte snippet', async () => {
    const detail = createRawSnippet((step: () => { id: string }) => ({ render: () => `<p>${step().id}</p>` }));
    expect(await astro(AstroStepListDetail, { steps: LIVE })).toBe(svelte(SvelteStepList, { steps: LIVE, detail }));
  });
});

describe('Spinner', () => {
  test('decorative ring, small by default, both adapters agree', async () => {
    expect(svelte(SvelteSpinner, {})).toBe('<span class="bb-spinner" aria-hidden="true"></span>');
    for (const props of [{}, { size: 'md', class: 'x' }]) {
      expect(await astro(AstroSpinner, props)).toBe(svelte(SvelteSpinner, props));
    }
    expect(svelte(SvelteSpinner, { size: 'md' })).toBe('<span class="bb-spinner bb-spinner--md" aria-hidden="true"></span>');
  });
});

describe('LineSeries', () => {
  const points = [
    { at: 0, production: 1, trials: 2 },
    { at: 5000, production: 3, trials: null },
    { at: 10_000, production: 2, trials: 1 },
  ];
  const series = [
    { key: 'production', label: 'Production' },
    { key: 'trials', label: 'Trials', tone: 'warm', dashed: true },
  ];
  const props = {
    points,
    series,
    ariaLabel: 'Throughput',
    description: 'Recent minutes',
    unit: 'eps',
    emptyLabel: 'Sampling',
    formatValue: (v: number | null | undefined) => (v == null ? 'n/a' : `${v} eps`),
    formatTime: (at: number) => `t${at}`,
  };

  test('both adapters render the same static chart', async () => {
    const html = svelte(SvelteLineSeries, props);
    expect(await astro(AstroLineSeries, props)).toBe(html);
  });

  test('legend carries the latest value per series and the tone modifiers', () => {
    const html = svelte(SvelteLineSeries, props);
    expect(html).toContain(
      '<div class="bb-line-series__key bb-line-series__key--accent"><span class="bb-line-series__swatch" aria-hidden="true"></span><span>Production</span><strong>2 eps</strong></div>',
    );
    expect(html).toContain('<div class="bb-line-series__key bb-line-series__key--warm bb-line-series__key--dashed">');
    expect(html).toContain('<svg viewBox="0 0 800 260" role="img" aria-label="Throughput"><title>Throughput</title><desc>Recent minutes</desc>');
    expect(html).not.toContain('bb-line-series__empty');
    expect(html).not.toContain('bb-line-series__tooltip');
  });

  test('fewer than two plotted readings shows the empty label over the plot', () => {
    const html = svelte(SvelteLineSeries, { ...props, points: [points[0]] });
    expect(html).toContain('<div class="bb-line-series__empty">Sampling</div>');
  });
});
