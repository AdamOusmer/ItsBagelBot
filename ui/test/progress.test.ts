// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { render } from 'svelte/server';
import { createRawSnippet } from 'svelte';
import { normalise } from './normalise';
import ProgressBar from '../svelte/ProgressBar.svelte';
import StepList from '../svelte/StepList.svelte';
import LogTail from '../svelte/LogTail.svelte';

const bar = (cls: string, aria: string) =>
  `<div class="bb-progress ${cls}" role="progressbar" aria-label="Build" aria-valuemin="0" aria-valuemax="100" ${aria}><span class="bb-progress__fill"></span></div>`;

const BAR_CASES: {
  name: string;
  props: { value: number | null; tone?: 'neutral' | 'success' | 'warning' | 'error'; size?: 'sm' | 'md' };
  html: string;
}[] = [
  {
    name: 'determinate: whole percent for screen readers, raw fraction for the fill',
    props: { value: 0.4666, tone: 'success' },
    html: bar('bb-progress--success', 'aria-valuenow="47" style="--progress: 0.4666;"'),
  },
  {
    name: 'indeterminate: busy, no value, no --progress',
    props: { value: null, size: 'sm' },
    html: bar('bb-progress--neutral bb-progress--sm bb-progress--indeterminate', 'aria-busy="true"'),
  },
  {
    name: 'over 1 clamps to full',
    props: { value: 1.7, tone: 'error' },
    html: bar('bb-progress--error', 'aria-valuenow="100" style="--progress: 1;"'),
  },
  {
    name: 'NaN renders empty rather than aria-valuenow="NaN"',
    props: { value: Number.NaN, tone: 'warning' },
    html: bar('bb-progress--warning', 'aria-valuenow="0" style="--progress: 0;"'),
  },
];

for (const c of BAR_CASES) {
  test(`ProgressBar: ${c.name}`, () => {
    expect(normalise(render(ProgressBar, { props: { ...c.props, label: 'Build' } }).body)).toBe(c.html);
  });
}

type StepItem = { id: string; label: string; state: string; value?: number | null; meta?: string; href?: string };

const STEPS: StepItem[] = [
  { id: 'tag', label: 'Tag', state: 'succeeded', href: '/runs/1#tag' },
  { id: 'build', label: 'Build', state: 'running', value: 0.5, meta: '3/6 jobs' },
  { id: 'rollout', label: 'Rollout', state: 'pending' },
];

const detail = createRawSnippet((step: () => StepItem) => ({ render: () => `<p>${step().id}</p>` }));

test('StepList: ordered rows, every slot rendered, running row alone is current', () => {
  const html = normalise(render(StepList, { props: { steps: STEPS, detail } }).body);
  expect(html).toBe(
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

test('StepList: state words are props merged over the English defaults', () => {
  const html = normalise(
    render(StepList, { props: { steps: STEPS, stateLabels: { running: 'En cours' } } }).body,
  );
  const words = [...html.matchAll(/class="bb-step__state">([^<]*)</g)].map((m) => m[1]);
  expect(words).toEqual(['Succeeded', 'En cours', 'Pending']);
});

const LOG_CASES: { name: string; props: { lines: string[]; max?: number }; tail: string }[] = [
  { name: 'keeps the last max lines, newline-joined', props: { lines: ['a', 'b', 'c'], max: 2 }, tail: 'b\nc' },
  {
    name: 'defaults to the last 50',
    props: { lines: Array.from({ length: 60 }, (_, i) => `l${i}`) },
    tail: Array.from({ length: 50 }, (_, i) => `l${i + 10}`).join('\n'),
  },
  { name: 'fewer lines than max renders them all', props: { lines: ['only'] }, tail: 'only' },
];

for (const c of LOG_CASES) {
  test(`LogTail: ${c.name}`, () => {
    const body = render(LogTail, { props: { ...c.props, label: 'build / linux-arm64' } }).body;
    const match = /<pre class="bb-log" role="region" aria-label="build \/ linux-arm64" tabindex="0">([\s\S]*)<\/pre>/.exec(body);
    expect(match?.[1]).toBe(c.tail);
  });
}
