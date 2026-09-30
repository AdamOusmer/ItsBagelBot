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
import SvelteInput from '../svelte/Input.svelte';
import AstroInput from '../astro/Input.astro';
import SvelteLabel from '../svelte/Label.svelte';
import AstroLabel from '../astro/Label.astro';
import SvelteHeading from '../svelte/Heading.svelte';
import AstroHeading from '../astro/Heading.astro';
import SvelteChip from '../svelte/Chip.svelte';
import AstroChip from '../astro/Chip.astro';
import SvelteBadge from '../svelte/Badge.svelte';
import AstroBadge from '../astro/Badge.astro';
import SvelteTag from '../svelte/Tag.svelte';
import AstroTag from '../astro/Tag.astro';
import SvelteRadioGroup from '../svelte/RadioGroup.svelte';
import AstroRadioGroup from '../astro/RadioGroup.astro';
import SvelteSegmentedControl from '../svelte/SegmentedControl.svelte';
import AstroSegmentedControl from '../astro/SegmentedControl.astro';
import SvelteSwitchRow from '../svelte/SwitchRow.svelte';
import AstroSwitchRow from '../astro/SwitchRow.astro';
import SvelteSlider from '../svelte/Slider.svelte';
import AstroSlider from '../astro/Slider.astro';
import SvelteFileDrop from '../svelte/FileDrop.svelte';
import AstroFileDrop from '../astro/FileDrop.astro';
import SvelteLanguageSwitcher from '../svelte/LanguageSwitcher.svelte';
import AstroLanguageSwitcher from '../astro/LanguageSwitcher.astro';
import SveltePickerOption from '../svelte/PickerOption.svelte';
import AstroPickerOption from '../astro/PickerOption.astro';

type Adapter = unknown;

interface Case {
  name: string;
  svelte: Adapter;
  astro: Adapter;
  props: Record<string, unknown>;
  slot?: string;
  html: string;
}

const CHECK_ICON =
  '<svg class="bb-icon" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" ' +
  'stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6L9 17l-5-5"></svg>';

const X_ICON =
  '<svg class="bb-icon" viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" ' +
  'stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M18 6L6 18M6 6l12 12"></svg>';

function svelteHtml(component: Adapter, props: Record<string, unknown>, slot?: string): string {
  const withSlot = slot === undefined ? props : { ...props, children: createRawSnippet(() => ({ render: () => slot })) };
  return normalise(render(component as never, { props: withSlot as never }).body);
}

async function astroHtml(component: Adapter, props: Record<string, unknown>, slot?: string): Promise<string> {
  const container = await experimental_AstroContainer.create();
  const slots = slot === undefined ? {} : { slots: { default: slot } };
  return normalise(await container.renderToString(component as never, { props, ...slots }));
}

function contract({ name, svelte, astro, props, slot, html }: Case) {
  test(name, async () => {
    expect(svelteHtml(svelte, props, slot)).toBe(html);
    expect(await astroHtml(astro, props, slot)).toBe(html);
  });
}

const RELEASES = [{ value: 'v2', label: 'v2.0.0', description: 'abc1234', meta: '2h' }];
const KINDS = [
  { value: 'deploy', label: 'Deploy', description: 'Ship main', meta: '5 stages' },
  { value: 'rollback', label: 'Rollback', disabled: true },
];

describe('Button and ButtonLink', () => {
  contract({
    name: 'add is the dashed pill with no diamond mark',
    svelte: SvelteButton,
    astro: AstroButton,
    props: { variant: 'add' },
    slot: '+ Add line',
    html: '<button class="bb-btn bb-btn--add" type="button" data-mark><span class="bb-btn__content">+ Add line</span></button>',
  });

  contract({
    name: 'a disabled link drops its href and says so',
    svelte: SvelteButtonLink,
    astro: AstroButtonLink,
    props: { href: '/p?page=0', variant: 'ghost', disabled: true },
    slot: 'Prev',
    html: '<a class="bb-btn bb-btn--ghost" role="link" aria-disabled="true" data-mark><i class="bb-btn__mark" aria-hidden="true"></i><span class="bb-btn__content">Prev</span></a>',
  });
});

describe('Input', () => {
  contract({
    name: 'color gets the swatch modifier',
    svelte: SvelteInput,
    astro: AstroInput,
    props: { type: 'color', value: '#52b788', name: 'color' },
    html: '<span class="bb-input bb-input--color"><input type="color" value="#52b788" name="color"></span>',
  });

  contract({
    name: 'read-only is a native readonly input, styled by the contract',
    svelte: SvelteInput,
    astro: AstroInput,
    props: { value: 'Everyone', readonly: true, mono: true, fill: true },
    html: '<span class="bb-input bb-input--fill bb-input--mono"><input type="text" value="Everyone" readonly></span>',
  });
});

describe('Label and Heading', () => {
  contract({
    name: 'mono label',
    svelte: SvelteLabel,
    astro: AstroLabel,
    props: { mono: true, htmlFor: 'runs' },
    slot: 'Runs',
    html: '<label class="bb-label bb-label--mono" for="runs">Runs</label>',
  });

  contract({
    name: 'a legend label never carries for',
    svelte: SvelteLabel,
    astro: AstroLabel,
    props: { mono: true, as: 'legend', htmlFor: 'x' },
    slot: 'Sources',
    html: '<legend class="bb-label bb-label--mono">Sources</legend>',
  });

  contract({
    name: 'the element binding is not an attribute',
    svelte: SvelteHeading,
    astro: AstroHeading,
    props: { level: 1, tabindex: -1, id: 'h', element: null },
    slot: 'Step',
    html: '<h1 class="bb-h bb-h--l1" tabindex="-1" id="h">Step</h1>',
  });
});

describe('Chip, Badge and Tag tones', () => {
  contract({
    name: 'a static chip is a span with no button semantics',
    svelte: SvelteChip,
    astro: AstroChip,
    props: { as: 'span', tone: 'muted' },
    slot: '$(user)',
    html: '<span class="bb-chip bb-chip--muted">$(user)</span>',
  });

  contract({
    name: 'tier chip',
    svelte: SvelteChip,
    astro: AstroChip,
    props: { tone: 'vip', pressed: true, class: 'tier' },
    slot: 'vip',
    html: '<button type="button" class="bb-chip bb-chip--vip tier" aria-pressed="true" data-pressed>vip</button>',
  });

  contract({
    name: 'unpressed toggle chip',
    svelte: SvelteChip,
    astro: AstroChip,
    props: { pressed: false },
    slot: 'all',
    html: '<button type="button" class="bb-chip" aria-pressed="false">all</button>',
  });

  contract({
    name: 'action chip carries no pressed state',
    svelte: SvelteChip,
    astro: AstroChip,
    props: {},
    slot: 'go',
    html: '<button type="button" class="bb-chip">go</button>',
  });

  contract({
    name: 'span chip never claims aria-pressed',
    svelte: SvelteChip,
    astro: AstroChip,
    props: { as: 'span', pressed: true },
    slot: 'beta',
    html: '<span class="bb-chip" data-pressed>beta</span>',
  });

  contract({
    name: 'tier badge',
    svelte: SvelteBadge,
    astro: AstroBadge,
    props: { tone: 'paid', dashed: true },
    slot: 'Sub',
    html: '<span class="bb-tag bb-badge bb-tag--paid bb-badge--dashed">Sub</span>',
  });

  contract({
    name: 'status tag',
    svelte: SvelteTag,
    astro: AstroTag,
    props: { tone: 'warning' },
    slot: 'Drawn',
    html: '<span class="bb-tag bb-tag--warning">Drawn</span>',
  });
});

describe('RadioGroup', () => {
  contract({
    name: 'cards carry a tick, description and meta; disabled options stay in the group',
    svelte: SvelteRadioGroup,
    astro: AstroRadioGroup,
    props: { name: 'k', value: 'deploy', variant: 'cards', min: '150px', options: KINDS },
    html:
      '<div class="bb-choices bb-choices--cards" role="radiogroup" aria-label="Options" style="--choices-min: 150px;">' +
      '<label class="bb-choice"><input class="bb-choice__input" type="radio" name="k" value="deploy" checked>' +
      `<span class="bb-choice__top"><span class="bb-choice__tick" aria-hidden="true">${CHECK_ICON}</span></span>` +
      '<span class="bb-choice__label">Deploy</span><span class="bb-choice__desc">Ship main</span>' +
      '<span class="bb-choice__meta">5 stages</span></label>' +
      '<label class="bb-choice"><input class="bb-choice__input" type="radio" name="k" value="rollback" disabled>' +
      `<span class="bb-choice__top"><span class="bb-choice__tick" aria-hidden="true">${CHECK_ICON}</span></span>` +
      '<span class="bb-choice__label">Rollback</span></label></div>',
  });

  contract({
    name: 'rows keep the native radio',
    svelte: SvelteRadioGroup,
    astro: AstroRadioGroup,
    props: { name: 'r', value: 'v2', variant: 'rows', label: 'Rollback to', options: RELEASES },
    html:
      '<div class="bb-choices bb-choices--rows" role="radiogroup" aria-label="Rollback to">' +
      '<label class="bb-choice"><input class="bb-choice__input" type="radio" name="r" value="v2" checked>' +
      '<span class="bb-choice__label">v2.0.0</span><span class="bb-choice__desc">abc1234</span>' +
      '<span class="bb-choice__meta">2h</span></label></div>',
  });

  contract({
    name: 'tabs accept rich options and ignore their description',
    svelte: SvelteRadioGroup,
    astro: AstroRadioGroup,
    props: { name: 't', value: 'a', options: [{ value: 'a', label: 'A', description: 'x' }, { value: 'b', label: 'B', disabled: true }] },
    html:
      '<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label="Options">' +
      '<label class="bb-tab is-active"><input class="bb-tab__input" type="radio" name="t" value="a" checked> A</label>' +
      '<label class="bb-tab "><input class="bb-tab__input" type="radio" name="t" value="b" disabled> B</label></div>',
  });

  test('svelte cards render the leading snippet with the selected flag', () => {
    const leading = createRawSnippet((opt: () => { label: string }, on: () => boolean) => ({
      render: () => `<b>${opt().label[0]}${on() ? '*' : ''}</b>`,
    }));
    const html = svelteHtml(SvelteRadioGroup, { name: 'k', value: 'deploy', variant: 'cards', options: KINDS, leading });
    expect(html).toContain('<span class="bb-choice__lead"><b>D*</b></span>');
    expect(html).toContain('<span class="bb-choice__lead"><b>R</b></span>');
  });

  contract({
    name: 'cards take a fixed column count and a rail',
    svelte: SvelteRadioGroup,
    astro: AstroRadioGroup,
    props: { name: 'k', value: 'deploy', variant: 'cards', min: '168px', cols: 5, rail: 'md', options: [KINDS[0]] },
    html:
      '<div class="bb-choices bb-choices--cards bb-choices--cols bb-choices--rail-md" role="radiogroup" aria-label="Options" ' +
      'style="--choices-min: 168px; --choices-cols: 5;">' +
      '<label class="bb-choice"><input class="bb-choice__input" type="radio" name="k" value="deploy" checked>' +
      `<span class="bb-choice__top"><span class="bb-choice__tick" aria-hidden="true">${CHECK_ICON}</span></span>` +
      '<span class="bb-choice__label">Deploy</span><span class="bb-choice__desc">Ship main</span>' +
      '<span class="bb-choice__meta">5 stages</span></label></div>',
  });

  test('columns, rails and the card height hook are styled, rails after columns', async () => {
    const source = await Bun.file(new URL('../styles/elements/radio-group.css', import.meta.url)).text();
    expect(source).toContain('grid-template-columns: repeat(var(--choices-cols), minmax(0, 1fr));');
    for (const [size, width] of [['md', 980], ['sm', 560]] as const) {
      expect(source).toMatch(
        new RegExp(
          `@media \\(max-width: ${width}px\\) \\{\\s*\\.bb-choices--rail-${size} \\{[^}]*grid-template-columns: none;` +
            '[^}]*grid-auto-flow: column;[^}]*grid-auto-columns: minmax\\(var\\(--choices-min, 200px\\), 1fr\\);' +
            '[^}]*overflow-x: auto;[^}]*scroll-snap-type: x mandatory;',
        ),
      );
      expect(source.indexOf(`.bb-choices--rail-${size} {`)).toBeGreaterThan(source.indexOf('.bb-choices--cols {'));
    }
    expect(source).toContain('> .bb-choice { scroll-snap-align: start; }');
    expect(source).toMatch(/\.bb-choices--cards \.bb-choice \{[^}]*min-height: var\(--choice-min-h, auto\);/);
  });
});

contract({
  name: 'SegmentedControl: option objects carry counts beside plain strings',
  svelte: SvelteSegmentedControl,
  astro: AstroSegmentedControl,
  props: {
    value: 'all',
    label: 'Source',
    options: [{ value: 'all', label: 'All', count: 12 }, { value: 'mod', label: 'Modules', count: 0 }, 'Plain'],
  },
  html:
    '<div class="bb-tabs bb-tabs--wrap" role="radiogroup" aria-label="Source">' +
    '<button type="button" class="bb-tab is-active" role="radio" aria-checked="true" tabindex="0" value="all">All<span class="bb-tab__count">12</span></button>' +
    '<button type="button" class="bb-tab " role="radio" aria-checked="false" tabindex="-1" value="mod">Modules<span class="bb-tab__count">0</span></button>' +
    '<button type="button" class="bb-tab " role="radio" aria-checked="false" tabindex="-1" value="Plain">Plain</button></div>',
});

describe('SwitchRow', () => {
  contract({
    name: 'switch first, hint wired to the switch',
    svelte: SvelteSwitchRow,
    astro: AstroSwitchRow,
    props: { label: 'Enabled', hint: 'Runs in chat', hintId: 'h1', checked: true },
    html:
      '<div class="bb-switch-row"><button type="button" class="bb-switch" role="switch" aria-checked="true" ' +
      'aria-label="Enabled" aria-describedby="h1" data-state="on"></button><span class="bb-switch-row__text">' +
      '<span class="bb-switch-row__label">Enabled</span><span class="bb-switch-row__hint" id="h1">Runs in chat</span></span></div>',
  });

  contract({
    name: 'switch last, own accessible name, submit type',
    svelte: SvelteSwitchRow,
    astro: AstroSwitchRow,
    props: { label: 'Streamer points', hint: 'Earn too', hintId: 'h2', control: 'end', switchLabel: 'Points', type: 'submit' },
    html:
      '<div class="bb-switch-row bb-switch-row--end"><span class="bb-switch-row__text">' +
      '<span class="bb-switch-row__label">Streamer points</span><span class="bb-switch-row__hint" id="h2">Earn too</span></span>' +
      '<button type="submit" class="bb-switch" role="switch" aria-checked="false" aria-label="Points" ' +
      'aria-describedby="h2" data-state="off"></button></div>',
  });

  test('the note renders on its own line after the switch in both adapters', async () => {
    const note = '<small>Refused</small>';
    const tail = 'data-state="off"></button><span class="bb-switch-row__note"><small>Refused</small></span></div>';
    const svelteNote = createRawSnippet(() => ({ render: () => note }));
    expect(svelteHtml(SvelteSwitchRow, { label: 'On', control: 'end', note: svelteNote }).endsWith(tail)).toBe(true);
    const container = await experimental_AstroContainer.create();
    const astro = normalise(
      await container.renderToString(AstroSwitchRow as never, { props: { label: 'On', control: 'end' }, slots: { note } }),
    );
    expect(astro.endsWith(tail)).toBe(true);
  });
});

contract({
  name: 'Slider is a range input',
  svelte: SvelteSlider,
  astro: AstroSlider,
  props: { value: 12, min: 1, max: 60, name: 'runs' },
  html: '<input type="range" class="bb-slider" min="1" max="60" step="1" value="12" name="runs">',
});

contract({
  name: 'FileDrop labels its hidden input with the hint',
  svelte: SvelteFileDrop,
  astro: AstroFileDrop,
  props: { label: 'Drop a CSV', accept: '.csv', name: 'file' },
  html:
    '<label class="bb-file-drop"><input type="file" class="bb-file-drop__input" accept=".csv" name="file">' +
    '<span class="bb-file-drop__text">Drop a CSV</span></label>',
});

describe('LanguageSwitcher', () => {
  const options = [
    { code: 'en', label: 'EN', current: true, title: 'English' },
    { code: 'fr', label: 'FR' },
  ];

  contract({
    name: 'form mode posts the chosen code',
    svelte: SvelteLanguageSwitcher,
    astro: AstroLanguageSwitcher,
    props: { label: 'Language', action: '/lang', name: 'to', fields: { next: '/x' }, options },
    html:
      '<form method="POST" action="/lang" class="bb-lang-switch" role="group" aria-label="Language">' +
      '<input type="hidden" name="next" value="/x">' +
      '<button type="submit" name="to" value="en" class="bb-lang-switch__opt is-active" aria-pressed="true" title="English">EN</button>' +
      '<button type="submit" name="to" value="fr" class="bb-lang-switch__opt " aria-pressed="false">FR</button></form>',
  });

  test('svelte callback mode renders plain buttons', () => {
    const html = svelteHtml(SvelteLanguageSwitcher, { label: 'Language', options, onSelect: () => {} });
    expect(html).toBe(
      '<div class="bb-lang-switch" role="group" aria-label="Language">' +
        '<button type="button" class="bb-lang-switch__opt is-active" aria-pressed="true" title="English">EN</button>' +
        '<button type="button" class="bb-lang-switch__opt " aria-pressed="false">FR</button></div>',
    );
  });
});

describe('PickerOption', () => {
  contract({
    name: 'stacked row in a list, armed remove shows its word',
    svelte: SveltePickerOption,
    astro: AstroPickerOption,
    props: {
      label: '$(fetch.weather)',
      description: 'data.temp',
      layout: 'stacked',
      as: 'li',
      remove: { label: 'Delete weather', armed: true, armedLabel: 'Delete' },
    },
    html:
      '<li class="bb-picker-option bb-picker-option--stacked"><button type="button" class="bb-picker-option__main">' +
      '<span class="bb-picker-option__label">$(fetch.weather)</span><span class="bb-picker-option__desc">data.temp</span></button>' +
      '<button type="button" class="bb-picker-option__remove" aria-label="Delete weather" data-armed>Delete</button></li>',
  });

  contract({
    name: 'selected and disabled, attributes land on the row button',
    svelte: SveltePickerOption,
    astro: AstroPickerOption,
    props: { label: 'deaths', description: 'channel', disabled: true, selected: true, title: 'tip', remove: { label: 'Delete' } },
    html:
      '<div class="bb-picker-option"><button type="button" class="bb-picker-option__main" aria-current="true" disabled title="tip">' +
      '<span class="bb-picker-option__label">deaths</span><span class="bb-picker-option__desc">channel</span></button>' +
      `<button type="button" class="bb-picker-option__remove" aria-label="Delete">${X_ICON}</button></div>`,
  });

  test('a custom body replaces the label in both adapters', async () => {
    const body = '<code>$(user)</code>';
    const expected =
      '<div class="bb-picker-option"><button type="button" class="bb-picker-option__main"><code>$(user)</code></button></div>';
    expect(svelteHtml(SveltePickerOption, { label: 'ignored' }, body)).toBe(expected);
    expect(await astroHtml(AstroPickerOption, { label: 'ignored' }, body)).toBe(expected);
  });
});

describe('control stylesheets', () => {
  const css = async (path: string) =>
    (await Bun.file(new URL(`../styles/${path}`, import.meta.url)).text()).replace(/\/\*[\s\S]*?\*\//g, '');

  test('error red comes from the status token, not a literal', async () => {
    for (const path of ['elements/field.css', 'elements/typography.css', 'elements/button.css', 'elements/tag.css', 'elements/chip.css']) {
      expect(await css(path)).not.toMatch(/#cf8a78/i);
    }
  });

  test('tier tones read the tier tokens, and vip stays silver', async () => {
    const tags = (await css('elements/tag.css')) + (await css('elements/chip.css'));
    for (const tier of ['free', 'paid', 'vip', 'banned', 'inactive']) {
      expect(tags).toContain(`--tone: var(--bb-tier-${tier});`);
    }
    expect(tags).not.toMatch(/#d9aaff|purple/i);
  });

  test('a chip shows its tier colour only while pressed', async () => {
    const chip = await css('elements/chip.css');
    expect(chip).toContain('.bb-chip[data-pressed]:is(.bb-chip--free, .bb-chip--paid, .bb-chip--vip, .bb-chip--banned, .bb-chip--inactive)');
  });
});
