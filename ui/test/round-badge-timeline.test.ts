// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe } from 'bun:test';
import { contracts } from './contract';
import SvelteRoundBadge from '../svelte/RoundBadge.svelte';
import AstroRoundBadge from '../astro/RoundBadge.astro';
import SvelteTimeline from '../svelte/Timeline.svelte';
import AstroTimeline from '../astro/Timeline.astro';
import SvelteTimelineItem from '../svelte/TimelineItem.svelte';
import AstroTimelineItem from '../astro/TimelineItem.astro';
import SvelteContainer from '../svelte/Container.svelte';
import AstroContainer from '../astro/Container.astro';

describe('RoundBadge', () => {
  contracts(SvelteRoundBadge, AstroRoundBadge, [
    {
      name: 'default badge is the 25px circle',
      slots: { default: '+' },
      html: '<span class="bb-round-badge">+</span>',
    },
    {
      name: 'size, solid and muted modifiers',
      props: { size: 'lg', solid: true, muted: true, class: 'step' },
      slots: { default: '01' },
      html: '<span class="bb-round-badge bb-round-badge--lg bb-round-badge--solid bb-round-badge--muted step">01</span>',
    },
  ]);
});

describe('Timeline', () => {
  contracts(SvelteTimeline, AstroTimeline, [
    {
      name: 'default rail fades from tan to green',
      slots: { default: '<li>a</li>' },
      html: '<ol class="bb-timeline"><li>a</li></ol>',
    },
    {
      name: 'bridge rail drawn on reveal',
      props: { rail: 'bridge', draw: true, as: 'ul' },
      slots: { default: '<li>a</li>' },
      html: '<ul class="bb-timeline bb-timeline--bridge bb-timeline--draw"><li>a</li></ul>',
    },
  ]);

  contracts(SvelteTimelineItem, AstroTimelineItem, [
    {
      name: 'item carries a decorative dot',
      slots: { default: 'x' },
      html: '<li class="bb-timeline__item"><span class="bb-timeline__dot" aria-hidden="true"></span>x</li>',
    },
    {
      name: 'item without a dot hosts its own marker',
      props: { dot: false },
      slots: { default: 'x' },
      html: '<li class="bb-timeline__item">x</li>',
    },
  ]);
});

describe('Container hero width', () => {
  contracts(SvelteContainer, AstroContainer, [
    {
      name: 'hero width',
      props: { width: 'hero' },
      slots: { default: 'x' },
      html: '<div class="bb-container bb-container--hero">x</div>',
    },
  ]);
});
