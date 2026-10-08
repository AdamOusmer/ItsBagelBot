// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { astroHtml, sourceContracts, svelteHtml } from './contract';
import SvelteStepper from '../svelte/Stepper.svelte';
import AstroStepper from '../astro/Stepper.astro';
import SvelteStepList from '../svelte/StepList.svelte';
import AstroStepList from '../astro/StepList.astro';
import AstroStepListDetail from './fixtures/step-list-detail.astro';
import SvelteSpinner from '../svelte/Spinner.svelte';
import AstroSpinner from '../astro/Spinner.astro';
import SvelteLineSeries from '../svelte/LineSeries.svelte';
import AstroLineSeries from '../astro/LineSeries.astro';
import LogTail from '../svelte/LogTail.svelte';

const WIZARD = [{ label: 'Welcome' }, { label: 'App', detail: 'Create it' }, { label: 'Keys' }];

const pip = '<span class="bb-stepper__pip" aria-hidden="true"></span>';
const num = (n: string) => `<span class="bb-stepper__num" aria-hidden="true">${n}</span>`;
const sr = (text: string) => `<span class="bb-sr-only">${text}</span>`;
const check =
  '<svg class="bb-icon" viewBox="0 0 24 24" width="11" height="11" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6L9 17l-5-5"></svg>';

describe('Stepper', () => {
  test('horizontal with onSelect: numbered buttons, current marked, steps past maxStep disabled', () => {
    const html = svelteHtml(SvelteStepper, { steps: WIZARD, current: 1, maxStep: 1, label: 'Step 2 of 3', onSelect: () => {} });
    expect(html).toBe(
      '<nav class="bb-stepper" aria-label="Step 2 of 3" style="--stepper-i: 1; --stepper-n: 3;">' +
        '<span class="bb-stepper__track" aria-hidden="true"><span class="bb-stepper__trail"></span></span>' +
        '<ol class="bb-stepper__list">' +
        `<li class="bb-stepper__item bb-stepper__item--done"><button type="button" class="bb-stepper__step">${num('01')}${pip}${sr('Welcome, Completed')}</button></li>` +
        `<li class="bb-stepper__item bb-stepper__item--current"><button type="button" class="bb-stepper__step" aria-current="step">${num('02')}${pip}${sr('App, Current step')}</button></li>` +
        `<li class="bb-stepper__item"><button type="button" class="bb-stepper__step" disabled>${num('03')}${pip}${sr('Keys, Upcoming')}</button></li>` +
        '</ol><span class="bb-stepper__glide" aria-hidden="true"></span></nav>',
    );
  });

  test('a negative current idles the trail and hides the glide', () => {
    const html = svelteHtml(SvelteStepper, { steps: WIZARD, current: -1, label: 'Steps', onSelect: () => {} });
    expect(html).toContain('style="--stepper-i: 0; --stepper-n: 3;"');
    expect(html).toContain('class="bb-stepper__track bb-stepper__track--idle"');
    expect(html).toContain('class="bb-stepper__glide bb-stepper__glide--hidden"');
    expect(html).not.toContain('aria-current');
  });

  test('horizontal without onSelect is a static indicator, identical in both adapters', async () => {
    const props = { steps: WIZARD, current: 2, label: 'Step 3 of 3' };
    const html = svelteHtml(SvelteStepper, props);
    expect(html).toContain(`<span class="bb-stepper__step" aria-current="step">${num('03')}${pip}${sr('Keys, Current step')}</span>`);
    expect(html).not.toContain('role="img"');
    expect(html).not.toContain('<button');
    expect(await astroHtml(AstroStepper, props)).toBe(html);
  });

  test('vertical: check on done steps, numbers after, connector on all but the last, detail when given', async () => {
    const props = { steps: WIZARD, current: 1, label: 'Stages', orientation: 'vertical' };
    const html = svelteHtml(SvelteStepper, props);
    expect(html).toBe(
      '<ol class="bb-stepper bb-stepper--vertical" aria-label="Stages">' +
        `<li class="bb-stepper__item bb-stepper__item--done"><span class="bb-stepper__gutter" aria-hidden="true"><span class="bb-stepper__dot">${check}</span><span class="bb-stepper__bar"></span></span>` +
        '<span class="bb-stepper__text"><span class="bb-stepper__title">Welcome</span>' + sr('Completed') + '</span></li>' +
        '<li class="bb-stepper__item bb-stepper__item--current" aria-current="step"><span class="bb-stepper__gutter" aria-hidden="true"><span class="bb-stepper__dot">2</span><span class="bb-stepper__bar"></span></span>' +
        '<span class="bb-stepper__text"><span class="bb-stepper__title">App</span>' + sr('Current step') + '<span class="bb-stepper__detail">Create it</span></span></li>' +
        '<li class="bb-stepper__item"><span class="bb-stepper__gutter" aria-hidden="true"><span class="bb-stepper__dot">3</span></span>' +
        '<span class="bb-stepper__text"><span class="bb-stepper__title">Keys</span>' + sr('Upcoming') + '</span></li></ol>',
    );
    expect(await astroHtml(AstroStepper, props)).toBe(html);
  });

  test('compact adds the modifier and the current label after the glide, hidden from assistive tech', () => {
    const html = svelteHtml(SvelteStepper, { steps: WIZARD, current: 1, maxStep: 1, label: 'Step 2 of 3', compact: true, onSelect: () => {} });
    expect(html.startsWith('<nav class="bb-stepper bb-stepper--compact" aria-label="Step 2 of 3"')).toBe(true);
    expect(html.endsWith('<span class="bb-stepper__glide" aria-hidden="true"></span><span class="bb-stepper__compact" aria-hidden="true">App</span></nav>')).toBe(true);
  });

  test('compact is opt-in: false renders exactly the default rail', () => {
    const props = { steps: WIZARD, current: 1, label: 'Step 2 of 3', onSelect: () => {} };
    const html = svelteHtml(SvelteStepper, { ...props, compact: false });
    expect(html).toBe(svelteHtml(SvelteStepper, props));
    expect(html).not.toContain('compact');
  });

  test('compact renders the same in both adapters, with an empty label before the first step', async () => {
    for (const current of [2, -1]) {
      const props = { steps: WIZARD, current, label: 'Steps', compact: true };
      expect(await astroHtml(AstroStepper, props)).toBe(svelteHtml(SvelteStepper, props));
    }
    expect(svelteHtml(SvelteStepper, { steps: WIZARD, current: -1, label: 'Steps', compact: true })).toContain(
      '<span class="bb-stepper__compact" aria-hidden="true"></span>',
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
  const html = svelteHtml(SvelteStepList, { steps: CHECKLIST, selected: 'build', label: 'Stages', onSelect: () => {} });

  test('onSelect turns the list into a labelled nav with a glide at the selected index', () => {
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
    const other = svelteHtml(SvelteStepList, { steps: CHECKLIST, selected: 'nope', onSelect: () => {} });
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
    expect(await astroHtml(AstroStepList, { steps: LIVE, stateLabels: { running: 'En cours' } })).toBe(
      svelteHtml(SvelteStepList, { steps: LIVE, stateLabels: { running: 'En cours' } }),
    );
  });

  test('a function child renders per-row detail like the Svelte snippet', async () => {
    const detail = createRawSnippet((step: () => { id: string }) => ({ render: () => `<p>${step().id}</p>` }));
    expect(await astroHtml(AstroStepListDetail, { steps: LIVE })).toBe(svelteHtml(SvelteStepList, { steps: LIVE, detail }));
  });
});

describe('Spinner', () => {
  test('decorative ring, small by default, both adapters agree', async () => {
    expect(svelteHtml(SvelteSpinner, {})).toBe('<span class="bb-spinner" aria-hidden="true"></span>');
    for (const props of [{}, { size: 'md', class: 'x' }]) {
      expect(await astroHtml(AstroSpinner, props)).toBe(svelteHtml(SvelteSpinner, props));
    }
    expect(svelteHtml(SvelteSpinner, { size: 'md' })).toBe('<span class="bb-spinner bb-spinner--md" aria-hidden="true"></span>');
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
    label: 'Throughput',
    description: 'Recent minutes',
    unit: 'eps',
    emptyLabel: 'Sampling',
    formatValue: (v: number | null | undefined) => (v == null ? 'n/a' : `${v} eps`),
    formatTime: (at: number) => `t${at}`,
  };

  test('both adapters render the same static chart', async () => {
    const html = svelteHtml(SvelteLineSeries, props);
    expect(await astroHtml(AstroLineSeries, props)).toBe(html);
  });

  test('legend carries the latest value per series and the tone modifiers', () => {
    const html = svelteHtml(SvelteLineSeries, props);
    expect(html).toContain(
      '<div class="bb-line-series__key bb-line-series__key--accent"><span class="bb-line-series__swatch" aria-hidden="true"></span><span>Production</span><strong>2 eps</strong></div>',
    );
    expect(html).toContain('<div class="bb-line-series__key bb-line-series__key--warm bb-line-series__key--dashed">');
    expect(html).toContain('<svg viewBox="0 0 800 260" role="img" aria-label="Throughput"><title>Throughput</title><desc>Recent minutes</desc>');
    expect(html).not.toContain('bb-line-series__empty');
    expect(html).not.toContain('bb-line-series__tooltip');
  });

  test('fewer than two plotted readings shows the empty label over the plot', () => {
    const html = svelteHtml(SvelteLineSeries, { ...props, points: [points[0]] });
    expect(html).toContain('<div class="bb-line-series__empty">Sampling</div>');
  });
});

type StepState = 'pending' | 'running' | 'waiting' | 'succeeded' | 'failed' | 'skipped' | 'cancelled';
type StepItem = { id: string; label: string; state: StepState; value?: number | null; meta?: string; href?: string };

describe('StepList static form', () => {
  const STEPS: StepItem[] = [
    { id: 'tag', label: 'Tag', state: 'succeeded', href: '/runs/1#tag' },
    { id: 'build', label: 'Build', state: 'running', value: 0.5, meta: '3/6 jobs' },
    { id: 'rollout', label: 'Rollout', state: 'pending' },
  ];

  test('ordered rows, every slot rendered, running row alone is current', () => {
    const detail = createRawSnippet((step: () => StepItem) => ({ render: () => `<p>${step().id}</p>` }));
    expect(svelteHtml(SvelteStepList, { steps: STEPS, detail })).toBe(
      '<ol class="bb-steps">' +
        '<li class="bb-step bb-step--succeeded"><div class="bb-step__row"><i class="bb-mark" aria-hidden="true"></i>' +
        '<span class="bb-step__text"><a class="bb-step__label" href="/runs/1#tag">Tag</a></span>' +
        '<div class="bb-step__bar"></div><span class="bb-step__state">Succeeded</span></div>' +
        '<div class="bb-step__detail"><p>tag</p></div></li>' +
        '<li class="bb-step bb-step--running" aria-current="step"><div class="bb-step__row"><i class="bb-mark" aria-hidden="true"></i>' +
        '<span class="bb-step__text"><span class="bb-step__label">Build</span><span class="bb-step__meta">3/6 jobs</span></span>' +
        '<div class="bb-step__bar"><div class="bb-progress bb-progress--neutral bb-progress--sm" role="progressbar" aria-label="Build" aria-valuemin="0" aria-valuemax="100" aria-valuenow="50" style="--progress: 0.5;"><span class="bb-progress__fill"></span></div></div>' +
        '<span class="bb-step__state">Running</span><i class="bb-sweep" aria-hidden="true"></i></div>' +
        '<div class="bb-step__detail"><p>build</p></div></li>' +
        '<li class="bb-step bb-step--pending"><div class="bb-step__row"><i class="bb-mark bb-mark--hollow" aria-hidden="true"></i>' +
        '<span class="bb-step__text"><span class="bb-step__label">Rollout</span></span>' +
        '<div class="bb-step__bar"></div><span class="bb-step__state">Pending</span></div>' +
        '<div class="bb-step__detail"><p>rollout</p></div></li>' +
        '</ol>',
    );
  });

  test('state words are props merged over the English defaults', () => {
    const html = svelteHtml(SvelteStepList, { steps: STEPS, stateLabels: { running: 'En cours' } });
    const words = [...html.matchAll(/class="bb-step__state">([^<]*)</g)].map((m) => m[1]);
    expect(words).toEqual(['Succeeded', 'En cours', 'Pending']);
  });
});

describe('LogTail', () => {
  const LOG_CASES: { name: string; props: { lines: string[]; max?: number }; tail: string }[] = [
    { name: 'keeps the last max lines, newline-joined', props: { lines: ['a', 'b', 'c'], max: 2 }, tail: 'b\nc' },
    {
      name: 'defaults to the last 50',
      props: { lines: Array.from({ length: 60 }, (_, i) => `l${i}`) },
      tail: Array.from({ length: 50 }, (_, i) => `l${i + 10}`).join('\n'),
    },
    { name: 'fewer lines than max renders them all', props: { lines: ['only'] }, tail: 'only' },
  ];

  for (const { name, props, tail } of LOG_CASES) {
    test(name, () => {
      const body = render(LogTail, { props: { ...props, label: 'build / linux-arm64' } }).body;
      const match = /<pre class="bb-log" role="region" aria-label="build \/ linux-arm64" tabindex="0">([\s\S]*)<\/pre>/.exec(body);
      expect(match?.[1]).toBe(tail);
    });
  }
});

describe('step stylesheets', () => {
  sourceContracts([
    {
      name: 'compact CSS: the label reserves 44px in pale tan mono caps and replaces the cells at 760px',
      checks: [
        {
          file: 'styles/elements/stepper.css',
          has: [
            /\.bb-stepper__compact \{\s*display: none;\s*align-items: center;\s*min-height: 44px;\s*color: var\(--bb-tan-pale\);\s*font-family: var\(--bb-font-mono\);\s*font-size: 11px;\s*letter-spacing: 0\.1em;\s*text-transform: uppercase;\s*\}/,
            /@media \(max-width: 760px\) \{[^@]*\.bb-stepper--compact :is\(\.bb-stepper__list, \.bb-stepper__track, \.bb-stepper__glide\) \{\s*display: none;\s*\}\s*\.bb-stepper__compact \{\s*display: flex;\s*\}/,
          ],
        },
      ],
    },
  ]);
});
