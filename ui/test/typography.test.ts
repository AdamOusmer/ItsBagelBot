// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe } from 'bun:test';
import { contracts, sourceContracts } from './contract';
import SvelteText from '../svelte/Text.svelte';
import AstroText from '../astro/Text.astro';
import SvelteEyebrow from '../svelte/Eyebrow.svelte';
import AstroEyebrow from '../astro/Eyebrow.astro';
import SvelteHeading from '../svelte/Heading.svelte';
import AstroHeading from '../astro/Heading.astro';
import SvelteCode from '../svelte/Code.svelte';
import AstroCode from '../astro/Code.astro';

const tone = (name: string) => ({
  name: `tone ${name}`,
  props: { tone: name, size: 'sm' },
  slots: { default: 'Copy' },
  html: `<p class="bb-text bb-text--sm bb-text--${name}">Copy</p>`,
});

describe('Text, Eyebrow, Heading and Code', () => {
  contracts(SvelteText, AstroText, [
    ...['muted-light', 'muted-soft', 'soft', 'positive', 'warn'].map(tone),
    {
      name: 'truncate sits after mono and before the caller class',
      props: { as: 'span', size: 'sm', mono: true, truncate: true, class: 'cell' },
      slots: { default: 'A long response' },
      html: '<span class="bb-text bb-text--sm bb-text--mono bb-text--truncate cell">A long response</span>',
    },
  ]);

  contracts(SvelteEyebrow, AstroEyebrow, [
    { name: 'default stays tan with no modifier', slots: { default: 'Live' }, html: '<span class="bb-eyebrow">Live</span>' },
    {
      name: 'go tone',
      props: { tone: 'go', as: 'p' },
      slots: { default: 'Live' },
      html: '<p class="bb-eyebrow bb-eyebrow--go">Live</p>',
    },
  ]);

  contracts(SvelteHeading, AstroHeading, [
    {
      name: 'title variant keeps the level element',
      props: { level: 5, as: 'h3', variant: 'title' },
      slots: { default: 'Timers' },
      html: '<h3 class="bb-h bb-h--l5 bb-h--title">Timers</h3>',
    },
    {
      name: 'label variant on a real heading',
      props: { level: 3, variant: 'label' },
      slots: { default: 'History' },
      html: '<h3 class="bb-h bb-h--l3 bb-h--label">History</h3>',
    },
    {
      name: 'uppercase follows the variant',
      props: { level: 5, as: 'h3', variant: 'title', uppercase: true, class: 'tier__name' },
      slots: { default: 'Free' },
      html: '<h3 class="bb-h bb-h--l5 bb-h--title bb-h--upper tier__name">Free</h3>',
    },
    {
      name: 'the element binding is not an attribute',
      props: { level: 1, tabindex: -1, id: 'h', element: null },
      slots: { default: 'Step' },
      html: '<h1 class="bb-h bb-h--l1" tabindex="-1" id="h">Step</h1>',
    },
  ]);

  contracts(SvelteCode, AstroCode, [
    {
      name: 'positive inline tone',
      props: { tone: 'success' },
      slots: { default: '{counter:target:deaths}' },
      html: '<code class="bb-code bb-code--success">{counter:target:deaths}</code>',
    },
    {
      name: 'wrapping block with a max height',
      props: { block: true, wrap: true, maxHeight: '150px', 'data-output': '' },
      slots: { default: '!hello' },
      html: '<pre class="bb-code-block bb-code-block--wrap" style="max-height:150px" data-output><code class="bb-code">!hello</code></pre>',
    },
    {
      name: 'plain block is unchanged',
      props: { block: true },
      slots: { default: 'x' },
      html: '<pre class="bb-code-block"><code class="bb-code">x</code></pre>',
    },
  ]);
});

describe('typography stylesheets', () => {
  sourceContracts([
    {
      name: 'every tone is a token colour and warn has no literal',
      checks: [
        {
          file: 'styles/elements/typography.css',
          has: [
            '.bb-text--muted-light { color: var(--bb-muted-light); }',
            '.bb-text--muted-soft { color: var(--bb-muted-soft); }',
            '.bb-text--success { color: var(--bb-green-glow); }',
            '.bb-text--warning { color: var(--bb-warn); }',
          ],
          lacks: [/#f2c879/i],
        },
        { file: 'styles/brand.css', has: ['--bb-warn: #f2c879;'] },
      ],
    },
    {
      name: 'truncate wins over the base wrap rule',
      checks: [
        { file: 'styles/elements/typography.css', after: [['.bb-text--truncate {', '.bb-text {']] },
        { file: 'styles/elements/typography.css', between: ['.bb-text--truncate {', '}'], has: ['white-space: nowrap;'] },
      ],
    },
    {
      name: 'label variant reproduces the admin block label',
      checks: [
        {
          file: 'styles/elements/typography.css',
          between: ['.bb-h--label {', '}'],
          has: [
            'font-family: var(--bb-font-mono);',
            'font-size: var(--h-label-size, 10px);',
            'letter-spacing: 0.12em;',
            'text-transform: uppercase;',
            'color: var(--bb-muted);',
          ],
        },
      ],
    },
    {
      name: 'programmatic focus drops the outline and keyboard focus keeps it',
      checks: [
        {
          file: 'styles/elements/typography.css',
          has: ['.bb-h:focus:not(:focus-visible) { outline: none; }'],
          lacks: [/\.bb-h:focus-visible[^{]*\{[^}]*outline:\s*none/],
        },
      ],
    },
    {
      name: 'small print sizes are hooks with the old defaults',
      checks: [
        { file: 'styles/elements/copy-surface.css', has: ['font-size: var(--copy-label-size, 10.5px);'] },
        { file: 'styles/elements/table.css', has: ['font-size: var(--tbl-head-size, 11px);'] },
        {
          file: 'styles/elements/typography.css',
          has: ['font-size: var(--label-mono-size, 10px);', 'font-size: var(--h-label-size, 10px);'],
        },
        { file: 'styles/elements/badge.css', has: ['font-size: var(--badge-pill-size, 10px);'] },
        { file: 'styles/elements/tabs.css', has: ['.bb-tabs--wrap > .bb-tab { flex-shrink: 0; }'] },
      ],
    },
  ]);
});
