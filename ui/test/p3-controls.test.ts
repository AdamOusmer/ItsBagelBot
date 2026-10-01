// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { createRawSnippet } from 'svelte';
import { render } from 'svelte/server';
import { experimental_AstroContainer } from 'astro/container';
import { normalise } from './normalise';
import SvelteButton from '../svelte/Button.svelte';
import AstroButton from '../astro/Button.astro';
import SvelteButtonLink from '../svelte/ButtonLink.svelte';
import AstroButtonLink from '../astro/ButtonLink.astro';
import SvelteIconButton from '../svelte/IconButton.svelte';
import AstroIconButton from '../astro/IconButton.astro';
import SvelteInput from '../svelte/Input.svelte';
import AstroInput from '../astro/Input.astro';
import SvelteSwitchRow from '../svelte/SwitchRow.svelte';
import AstroSwitchRow from '../astro/SwitchRow.astro';
import SvelteCard from '../svelte/Card.svelte';
import AstroCard from '../astro/Card.astro';
import SvelteProgressBar from '../svelte/ProgressBar.svelte';
import AstroProgressBar from '../astro/ProgressBar.astro';
import SvelteRadioGroup from '../svelte/RadioGroup.svelte';
import AstroRadioGroup from '../astro/RadioGroup.astro';
import SvelteSegmentedControl from '../svelte/SegmentedControl.svelte';
import AstroSegmentedControl from '../astro/SegmentedControl.astro';
import SvelteFact from '../svelte/Fact.svelte';
import AstroFact from '../astro/Fact.astro';
import SvelteFactList from '../svelte/FactList.svelte';
import AstroFactList from '../astro/FactList.astro';
import SvelteSection from '../svelte/Section.svelte';
import AstroSection from '../astro/Section.astro';
import SvelteTextLink from '../svelte/TextLink.svelte';
import AstroTextLink from '../astro/TextLink.astro';
import SvelteStatTile from '../svelte/StatTile.svelte';
import AstroStatTile from '../astro/StatTile.astro';

interface Case {
  name: string;
  svelte: unknown;
  astro: unknown;
  props: Record<string, unknown>;
  slots?: Record<string, string>;
  html: string;
}

function snippetsFor(slots: Record<string, string>) {
  const snippets: Record<string, unknown> = {};
  for (const [name, html] of Object.entries(slots)) {
    snippets[name === 'default' ? 'children' : name] = createRawSnippet(() => ({ render: () => html }));
  }
  return snippets;
}

function contract({ name, svelte, astro, props, slots = {}, html }: Case) {
  test(`${name}: svelte`, () => {
    const all = { ...props, ...snippetsFor(slots) };
    expect(normalise(render(svelte as never, { props: all as never }).body)).toBe(html);
  });

  test(`${name}: astro`, async () => {
    const container = await experimental_AstroContainer.create();
    expect(normalise(await container.renderToString(astro as never, { props, slots }))).toBe(html);
  });
}

const css = async (path: string) =>
  (await Bun.file(new URL(`../styles/${path}`, import.meta.url)).text()).replace(/\/\*[\s\S]*?\*\//g, '');

const MARK = '<i class="bb-btn__mark" aria-hidden="true"></i>';

describe('Button family', () => {
  contract({
    name: 'a static button is a span with no button semantics',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { as: 'span', variant: 'secondary', 'data-cursor': '' },
    slots: { default: 'Open guide' },
    html: `<span class="bb-btn bb-btn--secondary bb-btn--static" data-mark data-cursor>${MARK}<span class="bb-btn__content">Open guide</span></span>`,
  });

  contract({
    name: 'brand variant keeps the mark and reads its colours from hooks',
    svelte: SvelteButtonLink,
    astro: AstroButtonLink,
    props: { href: '/auth/login', variant: 'brand', block: true },
    slots: { default: 'Continue' },
    html: `<a class="bb-btn bb-btn--brand bb-btn--block" href="/auth/login" data-mark>${MARK}<span class="bb-btn__content">Continue</span></a>`,
  });

  contract({
    name: 'a danger icon button carries the red hover',
    svelte: SvelteIconButton,
    astro: AstroIconButton,
    props: { label: 'Delete deaths', size: 'sm', tone: 'danger' },
    slots: { default: '<svg></svg>' },
    html:
      '<button class="bb-btn bb-btn--icon bb-btn--sm bb-btn--danger-hover" type="button" aria-label="Delete deaths" data-mark>' +
      '<span class="bb-btn__content"><svg></svg></span></button>',
  });

  test('a static button follows the hover of the link around it, and brand reads only hooks', async () => {
    const button = await css('elements/button.css');
    expect(button).toContain('a:hover .bb-btn--static.bb-btn--primary {');
    expect(button).toContain('a:hover .bb-btn--static.bb-btn--secondary {');
    const brand = button.slice(button.indexOf('.bb-btn--brand {'), button.indexOf('.bb-btn--add {'));
    expect(brand).toContain('background: var(--btn-brand);');
    expect(brand).toContain('box-shadow: 0 0 24px var(--btn-brand-glow, transparent);');
    expect(brand).not.toMatch(/#[0-9a-f]{3,6}\b|rgba\(\d/i);
  });
});

contract({
  name: 'Input aligns numbers to the end',
  svelte: SvelteInput,
  astro: AstroInput,
  props: { align: 'end', value: '12', inputmode: 'numeric' },
  html: '<span class="bb-input bb-input--end"><input type="text" value="12" inputmode="numeric"></span>',
});

describe('SwitchRow', () => {
  contract({
    name: 'status sits between the text and the switch, warn tones the label',
    svelte: SvelteSwitchRow,
    astro: AstroSwitchRow,
    props: { label: 'Live only', control: 'end', tone: 'warning' },
    slots: { status: '<em>Saved</em>' },
    html:
      '<div class="bb-switch-row bb-switch-row--end bb-switch-row--warning"><span class="bb-switch-row__text">' +
      '<span class="bb-switch-row__label">Live only</span></span><span class="bb-switch-row__status"><em>Saved</em></span>' +
      '<button type="button" class="bb-switch" role="switch" aria-checked="false" aria-label="Live only" data-state="off"></button></div>',
  });

  test('warn reads the warn token', async () => {
    expect(await css('elements/toggle.css')).toContain(
      '.bb-switch-row--warning .bb-switch-row__label {\n        color: var(--bb-warn);',
    );
  });
});

describe('Card and facts honour hidden', () => {
  contract({
    name: 'dashed card',
    svelte: SvelteCard,
    astro: AstroCard,
    props: { dashed: true, hidden: true },
    slots: { default: 'Generate' },
    html: '<div class="bb-card bb-card--dashed" data-card hidden>Generate</div>',
  });

  contract({
    name: 'hidden fact',
    svelte: SvelteFact,
    astro: AstroFact,
    props: { term: 'Blocked', hidden: true },
    slots: { default: '3' },
    html: '<div class="bb-fact" hidden><dt class="bb-fact__term">Blocked</dt><dd class="bb-fact__value">3</dd></div>',
  });

  contract({
    name: 'hidden fact list',
    svelte: SvelteFactList,
    astro: AstroFactList,
    props: { layout: 'tiles', hidden: true },
    html: '<dl class="bb-facts bb-facts--tiles" hidden></dl>',
  });

  test('their own display rules yield to [hidden]', async () => {
    expect(await css('elements/card.css')).toContain('.bb-card[hidden] {\n    display: none;');
    expect(await css('elements/fact-list.css')).toContain(':is(.bb-facts, .bb-fact)[hidden] {\n    display: none;');
  });
});

describe('ProgressBar', () => {
  contract({
    name: 'gradient fill on a ramp step',
    svelte: SvelteProgressBar,
    astro: AstroProgressBar,
    props: { value: 0.5, tone: 'success', label: 'uptime', gradient: true, ramp: 2 },
    html:
      '<div class="bb-progress bb-progress--success bb-progress--gradient bb-progress--ramp-2" role="progressbar" ' +
      'aria-label="uptime" aria-valuemin="0" aria-valuemax="100" aria-valuenow="50" style="--progress: 0.5;">' +
      '<span class="bb-progress__fill"></span></div>',
  });

  contract({
    name: 'ramp 1 is the tone itself',
    svelte: SvelteProgressBar,
    astro: AstroProgressBar,
    props: { value: 1, tone: 'success', label: 'top', ramp: 1 },
    html:
      '<div class="bb-progress bb-progress--success" role="progressbar" aria-label="top" aria-valuemin="0" ' +
      'aria-valuemax="100" aria-valuenow="100" style="--progress: 1;"><span class="bb-progress__fill"></span></div>',
  });

  test('duration and gradient start are hooks with the old defaults', async () => {
    const bar = await css('elements/progress-bar.css');
    expect(bar).toContain('transition: transform var(--progress-duration, var(--bb-dur-base)) var(--bb-ease-out-expo);');
    expect(bar).toContain('var(--progress-from, color-mix(in srgb, var(--progress-fill) 50%, transparent))');
    expect(bar).toContain('.bb-progress--success.bb-progress--ramp-2 {\n    --progress-fill: var(--bb-green-light);');
    expect(bar).toContain('.bb-progress--success.bb-progress--ramp-3 {\n    --progress-fill: var(--bb-green);');
  });
});

describe('RadioGroup and SegmentedControl', () => {
  contract({
    name: 'rows scroll inside their own border',
    svelte: SvelteRadioGroup,
    astro: AstroRadioGroup,
    props: { name: 'r', value: 'v2', variant: 'rows', maxHeight: '360px', options: [{ value: 'v2', label: 'v2.0.0' }] },
    html:
      '<div class="bb-choices bb-choices--rows bb-choices--scroll bb-scroll" role="radiogroup" aria-label="Options" ' +
      'style="--choices-max-h: 360px;"><label class="bb-choice"><input class="bb-choice__input" type="radio" name="r" ' +
      'value="v2" checked><span class="bb-choice__label">v2.0.0</span></label></div>',
  });

  contract({
    name: 'segment buttons carry their value',
    svelte: SvelteSegmentedControl,
    astro: AstroSegmentedControl,
    props: { value: 'custom', label: 'Mode', options: [{ value: 'custom', label: 'Custom' }, 'plain'] },
    html:
      '<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label="Mode">' +
      '<button type="button" class="bb-tab is-active" role="radio" aria-checked="true" tabindex="0" value="custom"><span data-fit>Custom</span></button>' +
      '<button type="button" class="bb-tab " role="radio" aria-checked="false" tabindex="-1" value="plain"><span data-fit>plain</span></button></div>',
  });

  contract({
    name: 'segment options pass their own attributes to the button',
    svelte: SvelteSegmentedControl,
    astro: AstroSegmentedControl,
    props: {
      value: 'custom',
      label: 'Mode',
      options: [{ value: 'custom', label: 'Custom', attrs: { 'data-mode': 'custom' } }, 'plain'],
    },
    html:
      '<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label="Mode">' +
      '<button type="button" class="bb-tab is-active" role="radio" aria-checked="true" tabindex="0" value="custom" data-mode="custom"><span data-fit>Custom</span></button>' +
      '<button type="button" class="bb-tab " role="radio" aria-checked="false" tabindex="-1" value="plain"><span data-fit>plain</span></button></div>',
  });

  test('the astro tabs sync is-active with the checked radio', async () => {
    const source = await Bun.file(new URL('../astro/RadioGroup.astro', import.meta.url)).text();
    expect(source).toContain("document.addEventListener('change', syncTabs);");
    expect(source).toContain("classList.toggle('is-active', radio.checked)");
  });
});

describe('Section and page tokens', () => {
  contract({
    name: 'page rhythm',
    svelte: SvelteSection,
    astro: AstroSection,
    props: { size: 'page' },
    slots: { default: 'x' },
    html: '<section class="bb-section bb-section--page">x</section>',
  });

  test('section, hero and topbar tokens keep the values they replace', async () => {
    const brand = await css('brand.css');
    expect(brand).toContain('--bb-space-section: clamp(72px, 10vw, 120px);');
    expect(brand).toContain('--bb-space-section-sm: clamp(56px, 8vw, 88px);');
    expect(brand).toContain('--bb-content-hero: 720px;');
    expect(brand).toContain('--bb-topbar-height: calc(58px + env(safe-area-inset-top, 0px));');
    expect(brand).toContain('--bb-topbar-height: calc(52px + env(safe-area-inset-top, 0px));');
    const layout = await css('elements/layout.css');
    expect(layout).toContain('.bb-section--page { --section-pad: var(--bb-space-section); }');
    expect(layout).toContain('.bb-section--page { --section-pad: var(--bb-space-section-sm); }');
    const hero = await css('elements/page-hero.css');
    expect(hero).toContain('--page-hero-measure: var(--bb-content-hero);');
    expect(hero).not.toContain('--content-narrow');
  });
});

contract({
  name: 'TextLink quiet touch target',
  svelte: SvelteTextLink,
  astro: AstroTextLink,
  props: { href: '/modules', variant: 'quiet', touch: true, label: 'All modules' },
  html: '<a class="bb-link bb-link--quiet bb-link--touch" href="/modules">All modules</a>',
});

describe('StatTile', () => {
  contract({
    name: 'static tile skips the count-up',
    svelte: SvelteStatTile,
    astro: AstroStatTile,
    props: { label: 'Commands', value: '12', static: true },
    html:
      '<div class="bb-stat bb-stat--static"><div class="bb-stat__head"><span class="bb-stat__label">Commands</span></div>' +
      '<div class="bb-stat__value"><span>12</span></div></div>',
  });

  contract({
    name: 'inline toned stat',
    svelte: SvelteStatTile,
    astro: AstroStatTile,
    props: { label: 'Answered', value: '1,204', static: true, inline: true, tone: 'success' },
    html:
      '<div class="bb-stat bb-stat--static bb-stat--inline bb-stat--success"><div class="bb-stat__head">' +
      '<span class="bb-stat__label">Answered</span></div><div class="bb-stat__value"><span>1,204</span></div></div>',
  });

  test('only a moving tile lifts', async () => {
    expect(await css('elements/stat-tile.css')).toContain('.bb-stat:not(.bb-stat--static, .bb-stat--inline):hover {');
  });
});

describe('stagger hooks', () => {
  test('defaults render the old entrance', async () => {
    const reveal = await css('reveal.css');
    expect(reveal).toContain('var(--stagger-duration, 420ms)');
    expect(reveal).toContain('calc(var(--stagger-delay, 30ms) + var(--stagger-i) * var(--stagger-step, 60ms))');
    expect(reveal).toContain('var(--stagger-max, 260ms)');
    expect(reveal).toContain('translateX(calc(var(--bb-reveal-side, -1) * var(--bb-reveal-shift, 14px)))');
    expect(reveal).toContain('.bb-stagger > :nth-child(n + 8) { --stagger-i: 7; }');
  });
});
